#!/bin/bash
# Scenario A: upstream response is slow
# Expected: nginx rt ~0.5s, ngxray connect latency unchanged (~0.1ms)
set -e
cd "$(dirname "$0")/.."

echo "==> [Scenario A] upstream response delay = 0.5s"
make delay SECONDS=0.5
make clear-logs
make start-collect
sleep 2
make traffic N=20
sleep 6  # wait for collect flush
make stop-collect

echo ""
echo "=== ngxray ==="
make report

echo ""
echo "=== nginx ==="
python3 summarize_nginx.py

make delay SECONDS=0
