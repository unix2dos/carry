#!/usr/bin/env python3
"""Run the existing remote fixture checks through curl when urllib transport fails."""
import json
import subprocess
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import check

transport_retries = 0


def request(base, token, method, path, data=None, authenticated=True):
    global transport_retries
    if "\r" in token or "\n" in token:
        raise ValueError("invalid token header")
    headers = ["Content-Type: text/plain; charset=utf-8"]
    if authenticated:
        headers.append("Authorization: Bearer " + token)
    # Credentials go through stdin; curl does not follow redirects or skip TLS checks.
    config = "\n".join("header = " + json.dumps(h) for h in headers) + "\n"
    command = ["curl", "--disable", "--silent", "--show-error", "--config", "-",
               "--proto", "=http,https",
               "--connect-timeout", "5", "--max-time", "20",
               "--request", method, "--url", base.rstrip("/") + path,
               "--write-out", "\n%{http_code}"]
    with tempfile.NamedTemporaryFile() as body:
        if data is not None:
            body.write(data)
            body.flush()
            command += ["--data-binary", "@" + body.name]
        # Fixture PUT is an upsert; retry only idempotent requests on transport errors.
        # HTTP errors remain visible and are never retried here.
        for attempt in range(3):
            result = subprocess.run(command, input=config.encode(), capture_output=True, timeout=25)
            if result.returncode not in (6, 7, 28, 35, 52, 56) or method not in ("GET", "PUT", "DELETE") or attempt == 2:
                break
            transport_retries += 1
    if result.returncode:
        raise OSError(f"curl transport failed with exit code {result.returncode}")
    payload, status = result.stdout.rsplit(b"\n", 1)
    return int(status), json.loads(payload)


if __name__ == "__main__":
    if "--url" not in sys.argv:
        raise SystemExit("This adapter requires --url; use validation/check.py for local Docker checks.")
    check.request = request
    raise SystemExit(check.main())
