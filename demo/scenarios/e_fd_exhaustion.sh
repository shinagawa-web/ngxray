#!/bin/bash
# Scenario E: FD exhaustion early warning
# Add 'worker_rlimit_nofile 50;' to nginx.conf before running.
# Expected: ngxray fd shows high %, FDs/min rate, projected exhaustion time
set -e
cd "$(dirname "$0")/.."

echo "==> [Scenario E] FD exhaustion (requires worker_rlimit_nofile 50 in nginx.conf)"
echo "Make sure 'worker_rlimit_nofile 50;' is set in demo/nginx.conf, then press Enter."
read -r

make reload
make clear-logs
make start-collect
sleep 2

# Send concurrent requests to open many FDs simultaneously
CONTAINER=$(docker compose ps -q ngxray-demo)
docker exec "$CONTAINER" bash -c '
  for i in $(seq 1 40); do
    curl -s http://localhost/ >/dev/null &
  done
  wait
'

sleep 10
make stop-collect

echo ""
echo "=== ngxray ==="
make report

echo ""
echo "=== nginx ==="
python3 summarize_nginx.py
