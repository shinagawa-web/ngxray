#!/usr/bin/env python3
import http.server
import pathlib
import time

DELAY_FILE = pathlib.Path("/tmp/backend_delay")


class Handler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    def do_GET(self):
        try:
            delay = float(DELAY_FILE.read_text())
        except Exception:
            delay = 0.0
        time.sleep(delay)
        body = b"ok\n"
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_):
        pass


if __name__ == "__main__":
    http.server.HTTPServer(("", 8080), Handler).serve_forever()
