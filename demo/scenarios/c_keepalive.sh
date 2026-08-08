#!/bin/bash
# Scenario C: upstream keepalive reuse
# Expected without keepalive: connect count ≈ request count (20)
# Expected with keepalive:    connect count << request count (1-4)
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
cat <<'HINT'
Edit demo/nginx.conf: replace the upstream block with:

    upstream backend {
        server 127.0.0.1:8080;
        keepalive 4;
    }

    server {
        listen 80;
        location / {
            proxy_pass http://backend;
            proxy_http_version 1.1;
            proxy_set_header Connection "";
        }
    }

Then press Enter.
HINT
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
