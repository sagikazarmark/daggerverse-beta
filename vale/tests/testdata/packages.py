import io
from http.server import BaseHTTPRequestHandler, HTTPServer
from zipfile import ZipFile

archive = io.BytesIO()
with ZipFile(archive, "w") as package:
    package.writestr("Remote/.vale.ini", "[*.md]\nBasedOnStyles = Remote\n")
    package.writestr(
        "Remote/styles/Remote/Terms.yml",
        "extends: existence\nmessage: Use 'use' instead.\nlevel: error\ntokens:\n  - utilize\n",
    )


class Handler(BaseHTTPRequestHandler):
    downloads = 0

    def do_GET(self):
        if self.path == "/Remote.zip":
            Handler.downloads += 1
            body = archive.getvalue()
        elif self.path == "/count":
            body = str(Handler.downloads).encode()
        else:
            self.send_error(404)
            return
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


HTTPServer(("0.0.0.0", 8000), Handler).serve_forever()
