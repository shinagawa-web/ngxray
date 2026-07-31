# ngxray

See what your nginx is actually doing — from the outside, without touching it.

`ngxray` uses eBPF to observe a running nginx from the kernel side, so you get the
truth without editing a single line of config or issuing a reload.

*Grown out of [tinytap](https://github.com/shinagawa-web/tinytap), an eBPF
traffic-capture tool I built that already surfaces nginx request flows in
development. ngxray is the same approach, made safe to run in production.*

## Why

Access logs are written by nginx, after the response, at the application layer.
They can't show you:

- the real latency of upstream `connect()` — TCP retransmits and TLS handshake
  included, not the rough `$upstream_connect_time`
- whether keepalive is actually being reused, or nginx is reconnecting to the
  upstream on every request
- which worker generation is still holding connections after a reload

On long-running systems, the config says one thing and the kernel does another.
And the change you'd need to see the gap — editing `log_format` and reloading — is
exactly the change you're most afraid to make in production.

That's the whole point: the systems that most need observing are the ones you're
most afraid to touch. So ngxray touches nothing.

## What it shows (early scope)

1. **Real upstream connect latency** — measured from the kernel, not inferred from
   `$upstream_connect_time`.
2. **Worker generations** — which worker process is still serving connections after
   a reload.

Deliberately narrow: two things your access log can't show, without touching the
running server.

## Future directions

The same approach — reading the gap between what your config intends and what the
kernel actually does — extends naturally to other blind spots:

- **TLS handshake breakdown** — split upstream connect latency into TCP handshake
  and TLS handshake separately, so you know whether to tune session resumption or
  look at the upstream itself.
- **TIME_WAIT accumulation** — surface sockets piling up in TIME_WAIT on the
  nginx→upstream path, the real signal that keepalive isn't working despite being
  configured.
- **Upstream keepalive reuse ratio** — how often nginx opens a fresh connection
  to the upstream versus reusing one from the pool, measured directly instead of
  inferred from the TIME_WAIT symptom. Tells you whether the `keepalive` you
  configured is actually taking effect.
- **Slow client detection** — identify connections where nginx is blocked on
  `write()` to a slow client, giving you data to justify tightening
  `send_timeout`.
- **FD exhaustion early warning** — show file descriptor usage trending toward
  the process ulimit, before it becomes a wave of 502s.
- **Accept-queue overflow** — connections the kernel drops before nginx ever
  `accept()`s them: they never reach the access log at all, so a spike here is
  invisible from userspace. The evidence for tuning `listen ... backlog` and
  `net.core.somaxconn`.

The theme is the same throughout: give you the evidence to change a directive
with confidence, instead of tuning by instinct.

## How the features connect

The features are designed to work as a diagnostic chain, not just as independent measurements.

**FD exhaustion is the entry point.** When a worker's file descriptor count is climbing, the breakdown tells you where to look next:

- **Client sockets dominant** → slow clients are holding connections open. See slow client detection for per-connection blocked time and a basis for setting `send_timeout`.
- **Upstream sockets dominant** → nginx is not reusing upstream connections. Check TIME_WAIT accumulation for confirmation, then keepalive reuse ratio for the exact breakdown.
- **Growing after a reload** → old workers are still alive and holding connections. See worker generations for how long they've been lingering.

**The upstream-side chain:**

```
connect latency (high p99 + retransmits)
  → upstream is dropping SYNs
  → keepalive would reduce new connection frequency
      → TIME_WAIT accumulation (is keepalive actually working?)
          → keepalive reuse ratio (by how much?)
```

**The client-side chain:**

```
slow client detection (workers blocked on write)
  → workers can't accept new connections quickly
      → accept-queue drain time rises
  → client sockets pile up
      → FD exhaustion
```

**Worker generations** sits across both chains: after a reload, old workers holding long-lived connections inflate FD counts and mask the true state of the system until they drain.

## Correlation layer

ngxray doesn't try to re-derive everything from the kernel. nginx already writes
access and error logs, and for what those logs show well — status codes, request
URIs, the numbers nginx believes about itself — that's the right source. Rebuilding
them in eBPF would be a lot of effort to end up with a worse copy of what's already
on disk.

eBPF is for the other side: what the kernel *actually did*. The value shows up at
the seam between the two. ngxray reads the existing logs (read-only — never a
`log_format` change, never a reload) and joins them against the kernel-side view,
so you see the delta instead of two disconnected numbers:

- the access log says `$upstream_connect_time` was 5ms; the kernel says the connect
  took 200ms once TCP retransmits are counted.
- nginx logged a clean `200`; the kernel saw the response spend most of its time
  blocked on a `write()` to a slow client.
- the log shows a request served; the kernel shows which worker generation, and a
  reused or freshly opened upstream socket, served it.

That seam is the whole job — deliberately. ngxray is not a log aggregator and won't
compete with one: it leaves to the logs what the logs already do, and owns only the
contact point where config intent and kernel reality can finally be compared.
Without that join, every finding stays half a picture and the fix stays a guess.

The hard part is the correlation key — matching a kernel socket (4-tuple, PID,
timestamps) to a log line (client/upstream addresses, request), across keepalive
(many requests per connection) and HTTP/2 multiplexing. Getting that join right is
the actual work.

## Status

Early / design stage. **Not usable yet.**

⭐ Watch this repo to follow the progress.
