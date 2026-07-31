//go:build ignore

#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>

#define AF_INET         2
#define IPPROTO_TCP     6
#define TCP_ESTABLISHED 1
#define TCP_SYN_SENT    2
#define TCP_FIN_WAIT1   4
#define TCP_FIN_WAIT2   5
#define TCP_TIME_WAIT   6
#define TCP_CLOSE       7
#define TCP_CLOSE_WAIT  8
#define TCP_LAST_ACK    9
#define TCP_CLOSING    11

// In-flight connect state, keyed by sock* cast to u64.
struct connect_start {
	__u64 ts_ns;
	__u32 daddr;       // IPv4 destination address (network byte order)
	__u16 dport;       // destination port, host byte order (tracepoint applies ntohs)
	__u8  retransmits;
	__u8  _pad;
};

// Event emitted to userspace when a connection completes or fails.
struct connect_event {
	__u64 ts_ns;
	__u64 latency_ns;
	__u32 daddr;
	__u16 dport;
	__u8  failed;       // 1 if connection did not reach ESTABLISHED
	__u8  retransmits;
};

// Set of nginx worker TGIDs; populated and refreshed by userspace.
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 512);
	__type(key, __u32);
	__type(value, __u8);
} worker_pids SEC(".maps");

// In-flight connections keyed by sock pointer.
struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__uint(max_entries, 16384);
	__type(key, __u64);
	__type(value, struct connect_start);
} connect_map SEC(".maps");

// Ring buffer carrying completed connect_event records to userspace.
struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 1 << 20); // 1 MiB
} events SEC(".maps");

// Layout of /sys/kernel/tracing/events/sock/inet_sock_set_state/format.
struct inet_sock_set_state_ctx {
	unsigned short common_type;
	unsigned char  common_flags;
	unsigned char  common_preempt_count;
	int            common_pid;
	const void    *skaddr;
	int            oldstate;
	int            newstate;
	__u16          sport;
	__u16          dport;
	__u16          family;
	__u16          protocol;
	__u8           saddr[4];
	__u8           daddr[4];
	__u8           saddr_v6[16];
	__u8           daddr_v6[16];
};

// Layout of /sys/kernel/tracing/events/tcp/tcp_retransmit_skb/format.
struct tcp_retransmit_skb_ctx {
	unsigned short common_type;
	unsigned char  common_flags;
	unsigned char  common_preempt_count;
	int            common_pid;
	const void    *skbaddr;
	const void    *skaddr;
	int            state;
	__u16          sport;
	__u16          dport;
	__u16          family;
	__u8           saddr[4];
	__u8           daddr[4];
	__u8           saddr_v6[16];
	__u8           daddr_v6[16];
};

SEC("tracepoint/sock/inet_sock_set_state")
int handle_inet_sock_set_state(struct inet_sock_set_state_ctx *ctx)
{
	if (ctx->family != AF_INET || ctx->protocol != IPPROTO_TCP)
		return 0;

	__u64 skaddr = (__u64)(unsigned long)ctx->skaddr;

	if (ctx->newstate == TCP_SYN_SENT) {
		__u32 pid = bpf_get_current_pid_tgid() >> 32;
		__u8 *allowed = bpf_map_lookup_elem(&worker_pids, &pid);
		if (!allowed)
			return 0;

		__u32 daddr;
		__builtin_memcpy(&daddr, ctx->daddr, sizeof(daddr));
		struct connect_start cs = {
			.ts_ns       = bpf_ktime_get_ns(),
			.daddr       = daddr,
			.dport       = ctx->dport,
			.retransmits = 0,
		};
		bpf_map_update_elem(&connect_map, &skaddr, &cs, BPF_ANY);

	} else if (ctx->newstate == TCP_ESTABLISHED || ctx->newstate == TCP_CLOSE) {
		struct connect_start *cs = bpf_map_lookup_elem(&connect_map, &skaddr);
		if (!cs)
			return 0;

		struct connect_event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
		if (e) {
			e->ts_ns       = cs->ts_ns;
			e->latency_ns  = bpf_ktime_get_ns() - cs->ts_ns;
			e->daddr       = cs->daddr;
			e->dport       = ctx->dport;
			/* failed = 1 only for SYN_SENT→CLOSE; entries from other
			 * prior states shouldn't exist in connect_map, but be
			 * explicit to avoid mislabelling any unexpected path. */
			e->failed      = (ctx->oldstate == TCP_SYN_SENT &&
			                  ctx->newstate == TCP_CLOSE) ? 1 : 0;
			e->retransmits = cs->retransmits;
			bpf_ringbuf_submit(e, 0);
		}
		bpf_map_delete_elem(&connect_map, &skaddr);

	} else if (ctx->newstate == TCP_FIN_WAIT1  ||
	           ctx->newstate == TCP_FIN_WAIT2  ||
	           ctx->newstate == TCP_TIME_WAIT  ||
	           ctx->newstate == TCP_CLOSE_WAIT ||
	           ctx->newstate == TCP_LAST_ACK   ||
	           ctx->newstate == TCP_CLOSING) {
		/* Teardown states that should never appear before ESTABLISHED/CLOSE
		 * for a tracked connect, but delete defensively to prevent map
		 * exhaustion if the kernel transitions through an unexpected path. */
		bpf_map_delete_elem(&connect_map, &skaddr);
	}
	return 0;
}

SEC("tracepoint/tcp/tcp_retransmit_skb")
int handle_tcp_retransmit_skb(struct tcp_retransmit_skb_ctx *ctx)
{
	__u64 skaddr = (__u64)(unsigned long)ctx->skaddr;
	struct connect_start *cs = bpf_map_lookup_elem(&connect_map, &skaddr);
	if (!cs)
		return 0;
	if (cs->retransmits < 255)
		cs->retransmits++;
	return 0;
}

char LICENSE[] SEC("license") = "GPL";
