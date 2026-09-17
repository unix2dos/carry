#!/usr/bin/env python3
"""Local-only container validation; --url probes an explicitly selected fixture.

Only Python's standard library is required by this runner. No cloud resources
are created, no existing databases are used, and Docker must use a local socket.
"""
import argparse
import concurrent.futures
import datetime as dt
import json
import os
from pathlib import Path
import secrets
import shutil
import signal
import ssl
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parent
LANGUAGES = ("go", "python", "node", "rust")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *_args, **_kwargs):
        return None  # Never forward the fixture token to a redirect destination.


def request(base, token, method, path, data=None, authenticated=True):
    headers = {"Content-Type": "text/plain; charset=utf-8"}
    if authenticated:
        headers["Authorization"] = "Bearer " + token
    req = urllib.request.Request(base.rstrip("/") + path, data=data, headers=headers, method=method)
    opener = urllib.request.build_opener(NoRedirect(), urllib.request.HTTPSHandler(context=ssl.create_default_context()))
    try:
        with opener.open(req, timeout=12) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as exc:
        return exc.code, json.loads(exc.read())


def expect(base, token, method, path, status, data=None, authenticated=True):
    actual, body = request(base, token, method, path, data, authenticated)
    if actual != status:
        raise AssertionError(f"{method} {path}: expected HTTP {status}, got {actual}")
    return body


def probe(base, token, language, version, key, retain=False):
    health = expect(base, token, "GET", "/healthz", 200, authenticated=False)
    assert health == {"status": "ok", "language": language, "version": version}, "health metadata mismatch"
    assert expect(base, token, "GET", "/readyz", 200, authenticated=False) == {"status": "ready"}
    path = "/records/" + key
    value = "中文 🦀 '); DROP TABLE validation_records; --\nquoted \"value\""
    expect(base, token, "PUT", path, 401, value.encode(), authenticated=False)
    expect(base, token, "GET", path, 404)
    expect(base, token, "PUT", "/records/bad.key", 400, b"value")
    expect(base, token, "PUT", path, 413, b"a" * 4097)
    expect(base, token, "PUT", path, 400, b"\xff")
    expect(base, token, "PUT", path, 400, b"\x00")
    expect(base, token, "PUT", path, 400, b"")
    expect(base, token, "POST", path, 405, b"value")
    expected = {"key": key, "value": value}
    try:
        for _ in range(2):  # A retry of the same key must be an upsert.
            assert expect(base, token, "PUT", path, 200, value.encode()) == expected
        assert expect(base, token, "GET", path, 200) == expected, "database round trip mismatch"
        expected["value"] = "updated 🐍 " + language
        assert expect(base, token, "PUT", path, 200, expected["value"].encode()) == expected
        assert expect(base, token, "GET", path, 200) == expected
    finally:
        if not retain:
            expect(base, token, "DELETE", path, 200)
            expect(base, token, "GET", path, 404)
    return expected


def wait_until(fn, seconds=40):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        try:
            result = fn()
            if result:
                return result
        except (OSError, ValueError, urllib.error.URLError, AssertionError):
            pass
        time.sleep(0.3)
    raise TimeoutError("condition was not met within the validation timeout")


class Run:
    def __init__(self, args):
        self.args = args
        self.id = "pdv-" + secrets.token_hex(5)
        self.tmp = tempfile.TemporaryDirectory(prefix=self.id + "-")
        self.secret_dir = Path(self.tmp.name)
        self.out = ROOT / "artifacts" / self.id
        self.out.mkdir(parents=True)
        self.token, self.password = secrets.token_hex(24), secrets.token_hex(24)
        self.containers, self.images = [], {}
        self.network_created = False
        self.env = os.environ.copy()
        context = json.loads(subprocess.check_output(["docker", "context", "inspect"], text=True))[0]
        endpoint = self.env.get("DOCKER_HOST") or context["Endpoints"]["docker"]["Host"]
        if not endpoint.startswith("unix://"):
            raise RuntimeError("local validation requires a local Docker unix socket")
        for name in ("DOCKER_CONTEXT", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH"):
            self.env.pop(name, None)
        self.env["DOCKER_HOST"] = endpoint
        self.report = {"run_id": self.id, "started_at": dt.datetime.now(dt.timezone.utc).isoformat(),
                       "platform": args.platform, "checks": [], "cloud_executed": False,
                       "database": "two disposable local PostgreSQL containers", "status": "running"}

    def redact(self, text):
        return text.replace(self.token, "[TOKEN]").replace(self.password, "[PASSWORD]")

    def docker(self, *args, input=None, check=True, timeout=120, binary=False):
        result = subprocess.run(["docker", *args], env=self.env, input=input, capture_output=True,
                                text=not binary, timeout=timeout)
        if check and result.returncode:
            stderr = result.stderr.decode(errors="replace") if binary else result.stderr
            raise RuntimeError(self.redact(f"docker {args[0]} failed: {stderr[-1800:]}"))
        return result

    def passed(self, name):
        self.report["checks"].append({"name": name, "status": "passed"})
        print("PASS " + name, flush=True)

    def env_file(self, name, values):
        path = self.secret_dir / (name + ".env")
        for value in values.values():
            if "\n" in value or "\r" in value:
                raise ValueError("environment values must be single-line")
        path.write_text("".join(f"{k}={v}\n" for k, v in values.items()))
        path.chmod(0o600)
        return str(path)

    def build(self, language, revision="v1"):
        tag = f"personal-deploy-validation/{language}:{self.id}-{revision}"
        context = ROOT / "fixtures" / language
        if revision == "v2":
            context = self.secret_dir / (language + "-v2")
            shutil.copytree(ROOT / "fixtures" / language, context,
                            ignore=shutil.ignore_patterns("node_modules", "target", "__pycache__", ".env*"))
            source_name = {"go": "main.go", "python": "app.py", "node": "app.mjs", "rust": "src/main.rs"}[language]
            source = context / source_name
            text = source.read_text()
            assert text.count('"v1"') + text.count("'v1'") == 1, "version edit must target exactly one source literal"
            source.write_text(text.replace('"v1"', '"v2"').replace("'v1'", "'v2'"))
        path = self.out / f"build-{language}-{revision}.log"
        with path.open("w") as log:
            result = subprocess.run(["docker", "build", "--platform", self.args.platform,
                                     "--label", f"personal-deploy-validation={self.id}", "-t", tag,
                                     str(context)], env=self.env,
                                    stdout=log, stderr=subprocess.STDOUT, timeout=1200)
        if result.returncode:
            raise RuntimeError(f"{language} image build failed; see {path.name}")
        image_id = self.docker("image", "inspect", "--format", "{{.Id}}", tag).stdout.strip()
        self.images[language] = tag
        self.report.setdefault("images", {}).setdefault(language, {})[revision] = {"tag": tag, "id": image_id}
        print("BUILT " + language + " " + revision, flush=True)

    def start_db(self, suffix, alias):
        name = self.id + "-" + suffix
        self.containers.append(name)
        env_path = self.env_file(suffix, {"POSTGRES_USER": "validation", "POSTGRES_DB": "validation",
                                         "POSTGRES_PASSWORD": self.password})
        self.docker("run", "-d", "--name", name,
                    "--label", f"personal-deploy-validation={self.id}", "--network", self.id,
                    "--network-alias", alias, "--network-alias", alias + "-alt",
                    "--env-file", env_path, self.db_image)
        self.db_ready(name)
        return name

    def db_ready(self, name):
        wait_until(lambda: self.docker("exec", name, "pg_isready", "-h", "127.0.0.1",
                                      "-U", "validation", "-d", "validation", check=False).returncode == 0)

    def sql(self, name, query):
        return self.docker("exec", name, "psql", "-X", "-U", "validation", "-d", "validation",
                           "-v", "ON_ERROR_STOP=1", "-Atc", query).stdout.strip()

    def start_app(self, language, suffix="app", version=None, host="db", mode="disable", ca=False, port=8080, token=None):
        name = self.id + "-" + language + "-" + suffix
        self.containers.append(name)
        values = {"PORT": str(port), "VALIDATION_TOKEN": self.token if token is None else token,
                  "DATABASE_URL": f"postgresql://validation:{self.password}@{host}:5432/validation?sslmode=disable&ssl=0"}
        if version is not None:
            values["APP_VERSION"] = version
        if mode is not None:
            values["DATABASE_SSLMODE"] = mode
        if ca:
            values["DATABASE_CA_CERT"] = "/run/validation-ca.crt"
        command = ["run", "-d", "--name", name, "--label", f"personal-deploy-validation={self.id}",
                   "--network", self.id, "--platform", self.args.platform, "--env-file", self.env_file(name, values),
                   "-p", f"127.0.0.1::{port}", "--memory", "256m", "--cpus", "1"]
        if ca:
            command.extend(["--mount", f"type=bind,src={self.secret_dir / 'ca.crt'},dst=/run/validation-ca.crt,readonly"])
        self.docker(*command, self.images[language])
        address = self.docker("port", name, f"{port}/tcp").stdout.strip().splitlines()[0]
        if not address.startswith("127.0.0.1:"):
            raise RuntimeError("fixture port was not bound to loopback")
        return name, "http://" + address

    def app_ready(self, base):
        wait_until(lambda: request(base, self.token, "GET", "/readyz")[0] == 200)

    def expect_startup_failure(self, name, code="database_unavailable"):
        wait_until(lambda: self.docker("inspect", "--format", "{{.State.Status}}", name).stdout.strip() == "exited", seconds=20)
        exit_code = self.docker("inspect", "--format", "{{.State.ExitCode}}", name).stdout.strip()
        assert exit_code != "0", "rejected configuration must exit nonzero"
        logs = self.docker("logs", name)
        assert code in logs.stdout + logs.stderr, "startup failed for an unexpected reason"

    def save_logs(self, name):
        result = self.docker("logs", name, check=False)
        logs = result.stdout + result.stderr
        if self.token in logs or self.password in logs:
            self.report.setdefault("errors", []).append("credential found in fixture logs")
        (self.out / (name + ".log")).write_text(self.redact(logs))

    def enable_tls(self, db):
        ca_config = self.secret_dir / "ca.cnf"
        ca_config.write_text("[req]\nprompt=no\ndistinguished_name=dn\nx509_extensions=ca\n[dn]\nCN=Local Validation CA\n[ca]\nbasicConstraints=critical,CA:TRUE\nkeyUsage=critical,keyCertSign,cRLSign\n")
        extension = self.secret_dir / "server.ext"
        extension.write_text("basicConstraints=CA:FALSE\nkeyUsage=digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\nsubjectAltName=DNS:db\n")
        commands = [
            ["openssl", "req", "-x509", "-nodes", "-newkey", "rsa:2048", "-days", "1", "-keyout", "ca.key", "-out", "ca.crt", "-config", "ca.cnf"],
            ["openssl", "req", "-new", "-nodes", "-newkey", "rsa:2048", "-keyout", "server.key", "-out", "server.csr", "-subj", "/CN=db"],
            ["openssl", "x509", "-req", "-in", "server.csr", "-CA", "ca.crt", "-CAkey", "ca.key", "-CAcreateserial", "-days", "1", "-out", "server.crt", "-extfile", "server.ext"],
        ]
        for command in commands:
            subprocess.run(command, cwd=self.secret_dir, check=True, capture_output=True, timeout=30)
        for filename in ("server.key", "server.crt"):
            self.docker("cp", str(self.secret_dir / filename), f"{db}:/var/lib/postgresql/data/{filename}")
        self.docker("exec", "-u", "root", db, "chown", "postgres:postgres", "/var/lib/postgresql/data/server.key", "/var/lib/postgresql/data/server.crt")
        self.docker("exec", "-u", "root", db, "chmod", "600", "/var/lib/postgresql/data/server.key")
        self.sql(db, "ALTER SYSTEM SET ssl='on'")
        self.docker("restart", "-t", "3", db)
        self.db_ready(db)

    def run(self):
        self.docker("info", "--format", "{{.ServerVersion}}")
        inspected = self.docker("image", "inspect", self.args.postgres_image, check=False)
        if inspected.returncode:
            self.docker("pull", self.args.postgres_image, timeout=600)
            inspected = self.docker("image", "inspect", self.args.postgres_image)
        database_image = json.loads(inspected.stdout)[0]
        self.db_image = database_image["Id"]
        self.report["postgres_image"] = {"id": self.db_image, "architecture": database_image["Architecture"]}
        print(f"Local validation {self.id}, {self.args.platform}", flush=True)
        with concurrent.futures.ThreadPoolExecutor(max_workers=self.args.build_jobs) as pool:
            futures = [pool.submit(self.build, language) for language in LANGUAGES]
            for future in concurrent.futures.as_completed(futures):
                future.result()
        self.docker("network", "create", "--label", f"personal-deploy-validation={self.id}", self.id)
        self.network_created = True
        db = self.start_db("db", "db")
        apps, records = {}, {}
        for language in LANGUAGES:
            invalid, _ = self.start_app(language, "missing-token", token="")
            self.expect_startup_failure(invalid, "invalid_configuration")
            self.passed(language + ": startup rejects missing validation token")
            name, base = self.start_app(language)
            self.app_ready(base)
            key = self.id + "-" + language
            records[key] = probe(base, self.token, language, "v1", key, retain=True)["value"]
            self.passed(language + ": authenticated CRUD, retry, Unicode, SQL parameters, input bounds")
            self.save_logs(name)
            self.docker("rm", "-f", "-v", name)
            self.build(language, "v2")
            assert self.report["images"][language]["v1"]["id"] != self.report["images"][language]["v2"]["id"]
            name, base = self.start_app(language, "v2", port=8097)
            self.app_ready(base)
            assert expect(base, self.token, "GET", "/healthz", 200)["version"] == "v2"
            assert expect(base, self.token, "GET", "/records/" + key, 200)["value"] == records[key]
            apps[language] = (name, base)
            self.passed(language + ": changed source, rebuilt/replaced image, changed port, existing data retained")
        for language, (_, base) in apps.items():
            for key, value in records.items():
                assert expect(base, self.token, "GET", "/records/" + key, 200)["value"] == value
            self.passed(language + ": reads records written by all four languages")
        self.docker("stop", "-t", "3", db)
        for language, (_, base) in apps.items():
            expect(base, self.token, "GET", "/healthz", 200)
            assert expect(base, self.token, "GET", "/readyz", 503) == {"error": "database_unavailable"}
        self.docker("start", db)
        self.db_ready(db)
        for language, (_, base) in apps.items():
            self.app_ready(base)
            self.passed(language + ": database outage reported and recovery without app restart")
        for language in LANGUAGES:
            name, _ = self.start_app(language, "tls-required", mode=None)
            self.expect_startup_failure(name)
            self.passed(language + ": default TLS rejects plaintext database, despite URI sslmode=disable and ssl=0")
        self.enable_tls(db)
        for language in LANGUAGES:
            name, _ = self.start_app(language, "untrusted-ca", mode=None)
            self.expect_startup_failure(name)
            name, _ = self.start_app(language, "wrong-host", mode=None, ca=True, host="db-alt")
            self.expect_startup_failure(name)
            _, base = self.start_app(language, "trusted-ca", mode=None, ca=True)
            self.app_ready(base)
            assert expect(base, self.token, "GET", "/records/" + self.id + "-go", 200)["value"] == records[self.id + "-go"]
            self.passed(language + ": trusted TLS succeeds; untrusted certificate and wrong hostname rejected")
        # Stop every application writer before the final snapshot. Only our scratch containers are affected.
        for name in self.containers:
            if name != db:
                self.docker("stop", "-t", "1", name, check=False)
        snapshot = json.loads(self.sql(db, "SELECT json_object_agg(key,value) FROM validation_records"))
        assert snapshot == records, "unexpected records or duplicate/retry behavior"
        dump = self.docker("exec", db, "pg_dump", "-U", "validation", "-d", "validation", "--format=custom", "--no-owner", "--no-acl", binary=True).stdout
        target = self.start_db("target", "db-target")
        self.docker("exec", "-i", target, "pg_restore", "-U", "validation", "-d", "validation", "--exit-on-error", "--no-owner", "--no-acl", input=dump, binary=True)
        assert json.loads(self.sql(target, "SELECT json_object_agg(key,value) FROM validation_records")) == snapshot
        previous_id = int(self.sql(target, "SELECT max(id) FROM validation_records"))
        _, base = self.start_app("go", "migrated", host="db-target", version="migrated")
        self.app_ready(base)
        new_key = self.id + "-cutover"
        expect(base, self.token, "PUT", "/records/" + new_key, 200, b"target-only-write")
        assert self.sql(db, f"SELECT count(*) FROM validation_records WHERE key='{new_key}'") == "0"
        assert self.sql(target, f"SELECT count(*) FROM validation_records WHERE key='{new_key}'") == "1"
        assert int(self.sql(target, f"SELECT id FROM validation_records WHERE key='{new_key}'")) > previous_id
        self.passed("migration: stopped writers, pg_dump/restore, exact data match, identity sequence, target-only new write, source retained until cleanup")
        self.report["record_count_migrated"] = len(snapshot)
        self.report["status"] = "passed"

    def finish(self):
        cleanup_errors = []
        for name in self.containers:
            try:
                if self.docker("inspect", name, check=False).returncode:
                    continue
                self.save_logs(name)
                removed = self.docker("rm", "-f", "-v", name, check=False)
                if removed.returncode:
                    cleanup_errors.append(self.redact(removed.stderr))
            except Exception as exc:
                cleanup_errors.append(self.redact(str(exc)))
        if self.network_created:
            result = self.docker("network", "rm", self.id, check=False)
            if result.returncode:
                cleanup_errors.append(self.redact(result.stderr))
        self.tmp.cleanup()
        if cleanup_errors:
            self.report["cleanup_errors"] = cleanup_errors
            self.report["status"] = "failed"
        if self.report.get("errors"):
            self.report["status"] = "failed"
        self.report["finished_at"] = dt.datetime.now(dt.timezone.utc).isoformat()
        (self.out / "report.json").write_text(json.dumps(self.report, ensure_ascii=False, indent=2) + "\n")
        print(f"{self.report['status'].upper()}: {self.out / 'report.json'}", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--platform", default="linux/amd64")
    parser.add_argument("--build-jobs", type=int, choices=(1, 2, 3, 4), default=2)
    parser.add_argument("--postgres-image", default="postgres:16", help="Local scratch database image; its native architecture is independent of the app images")
    parser.add_argument("--url", help="Probe an existing fixture instead of starting local Docker resources")
    parser.add_argument("--language", choices=LANGUAGES)
    parser.add_argument("--version", default="v1")
    parser.add_argument("--allow-remote", action="store_true", help="Explicitly permit an HTTPS fixture probe with a temporary test write")
    args = parser.parse_args()
    if args.url:
        target = urllib.parse.urlsplit(args.url)
        local = target.hostname in ("127.0.0.1", "localhost", "::1")
        if target.username or target.password or target.query or target.fragment or target.path not in ("", "/"):
            parser.error("URL must be an origin without credentials, path, query or fragment")
        if target.scheme not in ("http", "https") or (not local and (target.scheme != "https" or not args.allow_remote)):
            parser.error("remote probes require HTTPS and --allow-remote")
        token = os.environ.get("VALIDATION_TOKEN", "")
        if len(token) < 32 or not args.language:
            parser.error("set VALIDATION_TOKEN (32+ bytes) and --language")
        probe(args.url, token, args.language, args.version, "probe-" + secrets.token_hex(12))
        print("PASS fixture probe; temporary record removed; no infrastructure was provisioned")
        return 0
    run = Run(args)
    try:
        run.run()
    except (Exception, KeyboardInterrupt) as exc:
        run.report["status"] = "failed"
        run.report["errors"] = [run.redact(f"{type(exc).__name__}: {exc}")]
        print(run.report["errors"][0], file=sys.stderr, flush=True)
    finally:
        run.finish()
    return 0 if run.report["status"] == "passed" else 1


if __name__ == "__main__":
    signal.signal(signal.SIGTERM, lambda *_: (_ for _ in ()).throw(KeyboardInterrupt()))
    sys.exit(main())
