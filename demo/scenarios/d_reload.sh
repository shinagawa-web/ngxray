#!/bin/bash
# Scenario D: nginx reload — worker drain time
# Expected: ngxray workers shows old worker PID and drain duration
set -e
cd "$(dirname "$0")/.."

echo "==> [Scenario D] nginx reload under traffic"
make clear-logs
make start-collect
sleep 2

make traffic N=10 &
TRAFFIC_PID=$!
sleep 2
make reload
wait $TRAFFIC_PID

make traffic N=10
sleep 6
make stop-collect

echo ""
echo "=== ngxray ==="
make report

echo ""
echo "=== nginx ==="
python3 summarize_nginx.py
