#!/bin/bash
set -e

mkdir -p /tmp/ngxray
echo 0 > /tmp/backend_delay

echo "==> Mounting tracefs..."
mount -t tracefs tracefs /sys/kernel/tracing 2>/dev/null || true

echo "==> Starting backend (port 8080)..."
python3 /backend.py &

echo "==> Starting nginx..."
echo "Ready. Use 'make' targets or scenario scripts to run experiments."
exec nginx -g "daemon off;"
