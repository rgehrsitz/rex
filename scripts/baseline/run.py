#!/usr/bin/env python3
"""Run REX-M0 checks and sequential benchmarks against a disposable Redis.

From repository root, provide a redis-server binary and a govulncheck binary
built with the repository's Go toolchain. Output must be a new directory.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import socket
import subprocess
import tempfile
import time


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--redis-server", type=Path, required=True)
    parser.add_argument("--govulncheck", type=Path, required=True)
    parser.add_argument("--runs", type=int, default=5)
    parser.add_argument("--memory-events", type=int, default=1000)
    parser.add_argument("--redis-events", type=int, default=200)
    parser.add_argument("--skip-checks", action="store_true", help="measurement-only follow-up; retain separate check evidence")
    parser.add_argument("--measurements", nargs="+", choices=["memory", "redis", "logging", "churn"], default=["memory", "redis", "logging", "churn"])
    args = parser.parse_args()
    if args.runs < 3 or any(n < 100 or n % 100 for n in [args.memory_events, args.redis_events]):
        parser.error("at least 3 runs and positive multiples of 100 events are required")
    root = Path(__file__).resolve().parents[2]
    os.chdir(root)
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=False)
    env = dict(os.environ, GOMAXPROCS="2")
    commands = []

    def capture(cmd):
        return subprocess.check_output(cmd, env=env, text=True).strip()

    def run(label, cmd, **kwargs):
        print(label, flush=True)
        started = time.time()
        with (out / (label + ".log")).open("w") as log:
            proc = subprocess.run(cmd, env=env, stdout=log, stderr=subprocess.STDOUT, **kwargs)
        commands.append({"check": label, "command": [str(v) for v in cmd], "exit_code": proc.returncode,
                         "elapsed_seconds": round(time.time() - started, 3)})
        (out / "commands.json").write_text(json.dumps(commands, indent=2) + "\n")
        if proc.returncode:
            raise RuntimeError(f"{label} failed; see {out / (label + '.log')}")

    revision = capture(["git", "rev-parse", "HEAD"])
    paths = capture(["git", "ls-files", "--cached", "--others", "--exclude-standard"]).splitlines()
    sources = sorted({p for p in paths if p.endswith(".go") or p in ("go.mod", "go.sum")
                      or p.startswith("scripts/baseline/")})
    manifest = "".join(f"{hashlib.sha256(Path(p).read_bytes()).hexdigest()}  {p}\n" for p in sources)
    fingerprint = hashlib.sha256(manifest.encode()).hexdigest()
    source_id = revision[:12] + "+" + fingerprint
    (out / "source-manifest.sha256").write_text(manifest)
    (out / "source.patch").write_text(capture(["git", "diff", "HEAD", "--", "pkg", "cmd", "go.mod", "go.sum", "examples/rex-rules-schema.json"]) + "\n")
    (out / "working-tree.txt").write_text(capture(["git", "status", "--short"]) + "\n")
    metadata = {"started_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                "revision": revision, "source_id": source_id, "platform": platform.platform(),
                "go": capture(["go", "version"]), "gomaxprocs": 2,
                "redis_binary": capture([str(args.redis_server.resolve()), "--version"]),
                "govulncheck_binary": capture(["go", "version", "-m", str(args.govulncheck.resolve())]),
                "runs": args.runs, "memory_events": args.memory_events, "redis_events": args.redis_events,
                "checks_skipped": args.skip_checks, "measurements": args.measurements,
                "warmup": 100, "logging": "disabled and info, both to io.Discard; standard logger to io.Discard",
                "redis_topology": "single disposable process, TCP IPv4 loopback, persistence disabled, no subscribers",
                "scope": "synchronous engine API batches; excludes ingress, JSON decoding, and derived-event consumption"}
    (out / "environment.json").write_text(json.dumps(metadata, indent=2) + "\n")
    with tempfile.TemporaryDirectory(prefix="rex-m0-") as temp:
        work = Path(temp)
        if not args.skip_checks:
            run("format", ["gofmt", "-l", "."])
            if (out / "format.log").read_text().strip():
                raise RuntimeError("gofmt reported unformatted files")
            run("tidy", ["go", "mod", "tidy", "-diff"])
            run("vet", ["go", "vet", "./..."])
            run("test", ["go", "test", "-count=1", "./..."])
            run("race", ["go", "test", "-race", "-count=1", "./..."])
            run("build", ["go", "build", "./..."])
            run("vulnerability", [str(args.govulncheck.resolve()), "-show", "verbose", "./..."])
            env.update(VERSION="v0.0.0-m0", DIST_DIR=str(work / "archives"))
            run("archives", ["bash", "scripts/release/build-archives.sh"])
            checksums = (work / "archives/checksums.txt").read_text()
            for line in checksums.splitlines():
                expected, filename = line.split()
                actual = hashlib.sha256((work / "archives" / filename).read_bytes()).hexdigest()
                if expected != actual:
                    raise RuntimeError(f"checksum mismatch for {filename}")
            (out / "archive-checksums.txt").write_text(checksums)
            (out / "archive-verification.txt").write_text(f"Verified {len(checksums.splitlines())} release archive SHA-256 checksums.\n")
        binary = work / "rex_baseline"
        run("harness-build", ["go", "build", "-o", str(binary), "./tools/rex_baseline"])
        # No validation/build tasks run concurrently with timed measurements.
        with socket.socket() as probe:
            probe.bind(("127.0.0.1", 0))
            port = probe.getsockname()[1]
        redis_cmd = [str(args.redis_server.resolve()), "--bind", "127.0.0.1", "--port", str(port),
                     "--save", "", "--appendonly", "no", "--daemonize", "no", "--dir", str(work)]
        with (out / "redis-server.log").open("w") as redis_log:
            server = subprocess.Popen(redis_cmd, stdout=redis_log, stderr=subprocess.STDOUT)
            try:
                for _ in range(100):
                    if server.poll() is not None:
                        raise RuntimeError("disposable Redis exited during startup")
                    # Verify ownership before allowing the harness to clear m0:*.
                    try:
                        with socket.create_connection(("127.0.0.1", port), timeout=0.2) as sock:
                            sock.sendall(b"*2\r\n$4\r\nINFO\r\n$6\r\nserver\r\n")
                            response = b""
                            while f"process_id:{server.pid}\r\n".encode() not in response:
                                chunk = sock.recv(65536)
                                if not chunk:
                                    break
                                response += chunk
                            if f"process_id:{server.pid}\r\n".encode() in response:
                                break
                    except (OSError, TimeoutError):
                        pass
                    time.sleep(0.1)
                else:
                    raise RuntimeError("could not verify disposable Redis ownership")
                common = [str(binary), "-source-id", source_id, "-runs", str(args.runs), "-warmup", "100"]
                if "memory" in args.measurements:
                    run("memory", common + ["-events", str(args.memory_events)])
                if "redis" in args.measurements:
                    run("redis", common + ["-mode", "redis", "-redis", f"127.0.0.1:{port}", "-events", str(args.redis_events)])
                if "logging" in args.measurements:
                    run("logging-info", common + ["-fixture", "shared-100", "-events", str(args.memory_events), "-log-level", "info"])
                if "churn" in args.measurements:
                    for cardinality in (1, 1000, 10000):
                        run(f"churn-{cardinality}", common + ["-fixture", "sparse-100", "-events", "10000", "-churn", str(cardinality)])
            finally:
                server.terminate()
                try:
                    server.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    server.kill()
                    server.wait()
    for name in ["memory", "redis", "logging-info", "churn-1", "churn-1000", "churn-10000"]:
        if (out / (name + ".log")).exists():
            (out / (name + ".log")).rename(out / (name + ".jsonl"))
    metadata["finished_utc"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    (out / "environment.json").write_text(json.dumps(metadata, indent=2) + "\n")
    print(f"Completed: {out}", flush=True)


if __name__ == "__main__":
    main()
