#!/bin/bash
# Scenario B: upstream TCP connect is slow (tc netem)
# Expected: nginx ct ~100ms, ngxray connect p99 ~100ms
set -e
cd "$(dirname "$0")/.."

echo "==> [Scenario B] adding 100ms latency on loopback"
make add-latency MS=100
make clear-logs
make start-collect
sleep 2
make traffic N=20
sleep 6
make stop-collect

echo ""
echo "=== ngxray ==="
make report

echo ""
echo "=== nginx ==="
python3 summarize_nginx.py

make remove-latency
