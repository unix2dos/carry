#!/usr/bin/env python3
"""Check an installed Carry from an empty state directory without cloud requests."""
import json
import os
from pathlib import Path
import selectors
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request


def main():
    if len(sys.argv) != 2:
        raise SystemExit("Usage: python3 validation/install-smoke.py INSTALL_DIRECTORY")
    installation = Path(sys.argv[1]).resolve()
    for tool, version in [("railway", "railway 5.57.2"), ("neon", "4.18.0"), ("vercel", "59.20.0")]:
        result = subprocess.run([str(installation / "tools/node_modules/.bin" / tool), "--version"],
                                capture_output=True, text=True, check=True, timeout=20)
        assert result.stdout.strip() == version, f"Unexpected {tool} version"
    with tempfile.TemporaryDirectory(prefix="carry-install-check-") as directory:
        state = Path(directory) / "state"
        command = [str(installation / "bin/carry"), "--state-dir", str(state)]
        result = subprocess.run(command + ["list"], cwd=directory, capture_output=True,
                                text=True, check=True, timeout=10)
        assert json.loads(result.stdout) == [], "Installation contains project state"
        process = subprocess.Popen(command + ["serve"], cwd=directory, stdout=subprocess.PIPE,
                                   stderr=subprocess.PIPE, text=True)
        try:
            with selectors.DefaultSelector() as selector:
                selector.register(process.stdout, selectors.EVENT_READ)
                received = bytearray()
                deadline = time.monotonic() + 10
                while True:
                    assert selector.select(timeout=max(0, deadline - time.monotonic())), "Server did not start"
                    chunk = os.read(process.stdout.fileno(), 4096)
                    assert chunk and len(received) + len(chunk) <= 4096, "Invalid startup response"
                    received.extend(chunk)
                    try:
                        address = json.loads(received)["url"]
                        break
                    except json.JSONDecodeError:
                        continue
            origin, key = address.split("#")
            client = urllib.request.build_opener(urllib.request.ProxyHandler({}))

            def get(path, headers=None):
                request = urllib.request.Request(origin + path, headers=headers or {})
                try:
                    with client.open(request, timeout=5) as response:
                        return response.status, response.read()
                except urllib.error.HTTPError as error:
                    with error:
                        return error.code, error.read()

            code, page = get("")
            assert code == 200 and b"Carry" in page
            assert get("api/projects")[0] == 401
            auth = {"Authorization": "Bearer " + key}
            code, data = get("api/projects", auth)
            assert code == 200 and json.loads(data) == []
            assert get("api/projects", {**auth, "Origin": "https://example.invalid"})[0] == 403
            assert state.stat().st_mode & 0o777 == 0o700
        finally:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
    print("PASS installed CLIs, empty state, Carry page, local authentication and origin boundary; no cloud requests")


if __name__ == "__main__":
    main()
