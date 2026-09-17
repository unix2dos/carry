"""Validation fixture, not a production application."""
import hmac
import json
import os
import re
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit

import psycopg

SCHEMA = "CREATE TABLE IF NOT EXISTS validation_records (id bigint GENERATED ALWAYS AS IDENTITY UNIQUE, key text PRIMARY KEY, value text NOT NULL)"


def main():
    token = os.environ.get("VALIDATION_TOKEN", "")
    mode = os.environ.get("DATABASE_SSLMODE", "verify-full")
    try:
        port = int(os.environ.get("PORT", "8080"))
        parts = urlsplit(os.environ["DATABASE_URL"])
        if not (1 <= port <= 65535 and len(token.encode()) >= 32 and parts.hostname
                and parts.scheme in ("postgres", "postgresql") and mode in ("disable", "verify-full")):
            raise ValueError()
        query = [(k, v) for k, v in parse_qsl(parts.query) if k not in
                 ("ssl", "sslmode", "sslrootcert", "sslcert", "sslkey", "uselibpqcompat")]
        dsn = urlunsplit(parts._replace(query=urlencode(query)))
    except (ValueError, KeyError):
        raise RuntimeError("invalid_configuration") from None
    options = {"sslmode": mode, "connect_timeout": 3, "autocommit": True,
               "options": "-c statement_timeout=5000"}
    if mode != "disable":
        options["sslrootcert"] = os.environ.get("DATABASE_CA_CERT", "/etc/ssl/certs/ca-certificates.crt")

    # ponytail: one connection per request for this low-rate fixture; add a pool for production throughput.
    def connect():
        return psycopg.connect(dsn, **options)

    try:
        with connect() as db:
            db.execute(SCHEMA)
    except Exception:
        raise RuntimeError("database_unavailable") from None
    version = os.environ.get("APP_VERSION", "v1")

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass  # Never put request data or credentials into fixture access logs.

        def setup(self):
            super().setup()
            self.connection.settimeout(10)

        def respond(self, status, body):
            data = json.dumps(body, ensure_ascii=False).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Cache-Control", "no-store")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def handle_request(self):
            path = urlsplit(self.path).path
            if path == "/healthz" and self.command == "GET":
                return self.respond(200, {"status": "ok", "language": "python", "version": version})
            ready = path == "/readyz" and self.command == "GET"
            if not ready:
                actual = self.headers.get("Authorization", "").encode()
                if not hmac.compare_digest(actual, ("Bearer " + token).encode()):
                    return self.respond(401, {"error": "unauthorized"})
                if not path.startswith("/records/"):
                    return self.respond(404, {"error": "not_found"})
                key = path[len("/records/"):]
                if not re.fullmatch(r"[A-Za-z0-9_-]{1,80}", key):
                    return self.respond(400, {"error": "invalid_key"})
                if self.command not in ("GET", "PUT", "DELETE"):
                    return self.respond(405, {"error": "method_not_allowed"})
            value = None
            if self.command == "PUT":
                try:
                    length = int(self.headers.get("Content-Length", "-1"))
                    if length > 4096:
                        return self.respond(413, {"error": "value_too_large"})
                    if length <= 0 or self.headers.get("Transfer-Encoding"):
                        raise ValueError()
                    data = self.rfile.read(length)
                    value = data.decode("utf-8", errors="strict")
                    if len(data) != length or "\x00" in value:
                        raise ValueError()
                except (ValueError, UnicodeError):
                    return self.respond(400, {"error": "invalid_value"})
            try:
                with connect() as db:
                    if ready:
                        db.execute("SELECT 1").fetchone()
                        return self.respond(200, {"status": "ready"})
                    if self.command == "PUT":
                        db.execute("INSERT INTO validation_records(key,value) VALUES(%s,%s) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value", (key, value))
                    elif self.command == "GET":
                        row = db.execute("SELECT value FROM validation_records WHERE key=%s", (key,)).fetchone()
                        if row is None:
                            return self.respond(404, {"error": "not_found"})
                        value = row[0]
                    else:
                        db.execute("DELETE FROM validation_records WHERE key=%s", (key,))
                        return self.respond(200, {"deleted": True})
                return self.respond(200, {"key": key, "value": value})
            except Exception:
                return self.respond(503, {"error": "database_unavailable"})

        do_GET = do_PUT = do_DELETE = do_POST = handle_request

    print(json.dumps({"event": "listening", "language": "python", "port": port}), flush=True)
    ThreadingHTTPServer(("0.0.0.0", port), Handler).serve_forever()


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        code = str(exc) if isinstance(exc, RuntimeError) else "startup_failed"
        print(json.dumps({"error": code}), file=sys.stderr)
        sys.exit(1)
