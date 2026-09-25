from http.server import BaseHTTPRequestHandler, HTTPServer


class Handler(BaseHTTPRequestHandler):
    requests = 0

    def do_GET(self):
        Handler.requests += 1
        self.send_response(200 if Handler.requests == 1 else 404)
        self.end_headers()
        self.wfile.write(b"OK")


HTTPServer(("0.0.0.0", 8000), Handler).serve_forever()
