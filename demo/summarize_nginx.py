#!/usr/bin/env python3
"""Summarize demo/logs/access.log.

Log format: '$remote_addr - [$time_local] "$request" $status ct=$upstream_connect_time rt=$upstream_response_time pid=$pid'
"""
import re
import sys
from pathlib import Path

LOG_FILE = Path(__file__).parent / "logs" / "access.log"
LINE_RE = re.compile(
    r'"[^"]+" (?P<status>\d+) ct=(?P<ct>[\d.]+|-) rt=(?P<rt>[\d.]+|-)'
)


def parse(path: Path) -> dict:
    statuses: dict[str, int] = {}
    ct_vals: list[float] = []
    rt_vals: list[float] = []

    for line in path.read_text().splitlines():
        m = LINE_RE.search(line)
        if not m:
            continue
        status = m.group("status")
        statuses[status] = statuses.get(status, 0) + 1
        if m.group("ct") != "-":
            ct_vals.append(float(m.group("ct")))
        if m.group("rt") != "-":
            rt_vals.append(float(m.group("rt")))

    return {"statuses": statuses, "ct": ct_vals, "rt": rt_vals}


def p99(vals: list[float]) -> float:
    if not vals:
        return 0.0
    s = sorted(vals)
    idx = max(0, int(len(s) * 0.99) - 1)
    return s[idx]


def avg(vals: list[float]) -> float:
    return sum(vals) / len(vals) if vals else 0.0


def main() -> None:
    path = Path(sys.argv[1]) if len(sys.argv) > 1 else LOG_FILE
    if not path.exists() or path.stat().st_size == 0:
        print("(no access log)")
        return

    d = parse(path)
    total = sum(d["statuses"].values())
    status_str = "  ".join(f"{k}:{v}" for k, v in sorted(d["statuses"].items()))

    print(f"  requests={total}  [{status_str}]")
    if d["ct"]:
        print(f"  upstream_connect_time   avg={avg(d['ct']):.3f}s  p99={p99(d['ct']):.3f}s")
    if d["rt"]:
        print(f"  upstream_response_time  avg={avg(d['rt']):.3f}s  p99={p99(d['rt']):.3f}s")


if __name__ == "__main__":
    main()
