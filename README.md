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
- **Per-upstream latency breakdown** — real connect time and time-to-first-byte
  per upstream server, not the aggregate your access log gives you.

The theme is the same throughout: give you the evidence to change a directive
with confidence, instead of tuning by instinct.

## Status

Early / design stage. **Not usable yet.**

⭐ Watch this repo to follow the progress.
