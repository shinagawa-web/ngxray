FROM golang:1.25 AS builder
RUN apt-get update && apt-get install -y clang llvm libbpf-dev linux-libc-dev
WORKDIR /src
COPY . .
RUN go generate ./internal/connect/bpf/
RUN go build -o /ngxray ./cmd/ngxray/

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y nginx python3 curl iproute2 procps && rm -rf /var/lib/apt/lists/*
COPY --from=builder /ngxray /usr/local/bin/ngxray
COPY demo/nginx.conf /etc/nginx/nginx.conf
COPY demo/ngxray.toml /etc/ngxray.toml
COPY demo/backend.py /backend.py
COPY demo/init.sh /init.sh
RUN chmod +x /init.sh
EXPOSE 80
ENTRYPOINT ["/init.sh"]
