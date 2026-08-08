CONTAINER := ngxray-demo
EXEC       := docker compose exec $(CONTAINER)
N          ?= 20
SECONDS    ?= 0
MS         ?= 100

.PHONY: up down reload traffic report clear-logs start-collect stop-collect add-latency remove-latency

up:
	docker compose up -d --build

down:
	docker compose down

reload:
	$(EXEC) nginx -s reload

traffic:
	$(EXEC) bash -c 'for i in $$(seq 1 $(N)); do curl -s http://localhost/ >/dev/null; done'

report:
	$(EXEC) ngxray report --config /etc/ngxray.toml

clear-logs:
	$(EXEC) bash -c '> /logs/access.log; > /logs/workers.ndjson; > /logs/fd.ndjson; > /logs/connect.ndjson'

start-collect:
	$(EXEC) bash -c 'ngxray collect --config /etc/ngxray.toml & echo $$! > /tmp/ngxray-collect.pid'

stop-collect:
	$(EXEC) bash -c 'kill $$(cat /tmp/ngxray-collect.pid 2>/dev/null) 2>/dev/null; rm -f /tmp/ngxray-collect.pid; true'

delay:
	$(EXEC) bash -c 'echo $(SECONDS) > /tmp/backend_delay'

add-latency:
	$(EXEC) bash -c 'tc qdisc add dev lo root netem delay $(MS)ms 2>/dev/null || tc qdisc change dev lo root netem delay $(MS)ms'

remove-latency:
	$(EXEC) bash -c 'tc qdisc del dev lo root 2>/dev/null || true'
