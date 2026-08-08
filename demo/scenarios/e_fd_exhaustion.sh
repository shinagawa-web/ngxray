#!/bin/bash
# Scenario E: FD exhaustion early warning
# Uses worker_rlimit_nofile 50 + slow backend to hold FDs open during snapshot.
# Expected: ngxray fd shows high %, FDs/min rate, projected exhaustion time
set -e
cd "$(dirname "$0")/.."

echo "==> [Scenario E] FD exhaustion"
echo "Make sure 'worker_rlimit_nofile 50;' is set in demo/nginx.conf, then press Enter."
read -r

make reload

# Slow backend holds connections open across multiple 5s collect intervals
make delay SECONDS=10

make clear-logs
make start-collect
sleep 2

# 20 concurrent requests each holding a connection for 2s
CONTAINER=$(docker compose ps -q ngxray-demo)
docker exec "$CONTAINER" bash -c '
  for i in $(seq 1 20); do
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

make delay SECONDS=0
