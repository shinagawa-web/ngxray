#!/bin/bash
# Scenario C: upstream keepalive reuse
# Run twice: once without keepalive (connect count ≈ requests), once with.
# Edit nginx.conf upstream block between runs to add/remove keepalive.
set -e
cd "$(dirname "$0")/.."

echo "==> [Scenario C] keepalive OFF (default)"
make clear-logs
make start-collect
sleep 2
make traffic N=20
sleep 6
make stop-collect

echo ""
echo "=== ngxray (no keepalive) ==="
make report

echo ""
echo "=== nginx ==="
python3 summarize_nginx.py

echo ""
echo "-------"
echo "Now add 'keepalive 4;' to the upstream block in demo/nginx.conf, then press Enter."
read -r

make reload
make clear-logs
make start-collect
sleep 2
make traffic N=20
sleep 6
make stop-collect

echo ""
echo "=== ngxray (with keepalive) ==="
make report

echo ""
echo "=== nginx ==="
python3 summarize_nginx.py
