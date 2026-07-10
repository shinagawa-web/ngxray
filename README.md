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

## Status

Early / design stage. **Not usable yet.**

⭐ Watch this repo to follow the progress.
