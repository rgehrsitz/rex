#!/usr/bin/env python3
"""Own a disposable loopback Redis and retain repeatable M8.7 evidence."""
import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import secrets
import shutil
import socket
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parents[2]
SCENARIOS = ("sparse-balanced", "dense-balanced", "sparse-skew",
             "completion-retry", "owner-loss")
MEASUREMENT_SOURCES = (
    ROOT / "pkg/runtime/durable_profile_test.go",
    Path(__file__).resolve(),
)


def command(args, **kwargs):
    return subprocess.check_output(args, cwd=ROOT, text=True, **kwargs).strip()


def executable_path(value, label):
    found = shutil.which(str(value))
    # Preserve the invoked symlink name: packaged Redis binaries may select
    # their operating mode from argv[0].
    path = Path(found).absolute() if found else Path(value).expanduser().absolute()
    if not path.is_file() or not os.access(path, os.X_OK):
        raise ValueError(f"{label} is not executable: {path}")
    return path


def percentiles(values):
    if not values:
        return {f"p{p}_ms": None for p in (50, 95, 99)}
    ordered = sorted(values)
    return {f"p{p}_ms": ordered[max(0, math.ceil(len(ordered)*p/100)-1)]/1e6
            for p in (50, 95, 99)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--redis-server", required=True)
    parser.add_argument("--redis-cli", type=Path,
                        help="redis-cli path; defaults to the redis-server directory")
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--runs", type=int, default=3)
    parser.add_argument("--events", type=int, default=1000)
    parser.add_argument("--race", action="store_true", help="correctness run; not comparable timing")
    parser.add_argument("--cpu-profile", action="store_true", help="retain Go CPU profile (instrumented timing)")
    args = parser.parse_args()
    if not 1 <= args.runs <= 10 or not 100 <= args.events <= 10000 or args.events % 100:
        parser.error("runs must be 1..10; events must be 100..10000 and a multiple of 100")
    try:
        server = executable_path(args.redis_server, "redis-server")
        cli = executable_path(args.redis_cli or server.with_name("redis-cli"), "redis-cli")
    except ValueError as err:
        parser.error(f"{err}; pass the executable path explicitly")
    args.output = args.output.resolve()
    args.output.mkdir(parents=True, exist_ok=False)
    changed_go = command(["git", "diff", "--name-only", "HEAD", "--", "*.go", "go.mod", "go.sum"])
    # Once committed, edits to the profile test are tracked and intentionally
    # trip changed_go; the untracked allowance supports initial milestone runs.
    untracked_go = command(["git", "ls-files", "--others", "--exclude-standard", "--", "*.go"])
    allowed_untracked_go = {
        str(path.relative_to(ROOT)) for path in MEASUREMENT_SOURCES
        if path.suffix == ".go"
    }
    unexpected_untracked_go = set(untracked_go.splitlines()) - allowed_untracked_go
    if changed_go or unexpected_untracked_go:
        raise RuntimeError("production Go/module sources must match HEAD; only the profile test may be untracked")
    metadata = {"revision": command(["git", "rev-parse", "HEAD"]),
                "status": command(["git", "status", "--short"]),
                "go": command(["go", "version"]), "platform": platform.platform(),
                "redis": command([str(server), "--version"]), "gomaxprocs": 2,
                "redis_cli": command([str(cli), "--version"]),
                "events_per_scenario": args.events, "runs": args.runs,
                "scenarios": SCENARIOS,
                "race": args.race, "cpu_profile": args.cpu_profile,
                "persistence": "disabled: save empty, appendonly no", "network": "loopback TCP",
                "logging": "error; condition tracing disabled",
                "warmup": "100 successful events per scenario; excluded from measurements",
                "scheduler": "one serial round-robin driver across four partitions",
                "production_source": "tracked Go/module sources at revision; profile test hashed separately",
                "sha256": {
                    str(path.relative_to(ROOT)): hashlib.sha256(path.read_bytes()).hexdigest()
                    for path in MEASUREMENT_SOURCES
                }}
    (args.output/"metadata.json").write_text(json.dumps(metadata, indent=2)+"\n")
    binary = args.output/"runtime.test"
    build = ["go", "test", "-c", "-o", str(binary)]
    if args.race:
        build.append("-race")
    subprocess.run(build+["./pkg/runtime"], cwd=ROOT, check=True)
    summary = []
    for run in range(args.runs):
      for scenario in SCENARIOS:
        stem = f"run-{run}-{scenario}"
        with tempfile.TemporaryDirectory(prefix="rex-m87-") as directory:
          # Redis bind failure is fatal; never attach to an occupied port.
          with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
          with (args.output/f"redis-{stem}.log").open("w") as log:
            proc = subprocess.Popen([str(server), "--bind", "127.0.0.1", "--port", str(port),
                "--save", "", "--appendonly", "no", "--dir", directory], stdout=log, stderr=log)
            try:
              def redis(*words):
                return command([str(cli), "-h", "127.0.0.1", "-p", str(port), *words], stderr=subprocess.DEVNULL)
              for _ in range(100):
                if proc.poll() is not None:
                  raise RuntimeError("owned Redis exited during startup")
                try:
                  info = redis("INFO", "server")
                  if f"process_id:{proc.pid}\n" in info.replace("\r", ""):
                    break
                except subprocess.CalledProcessError:
                  pass
                time.sleep(.05)
              else:
                raise RuntimeError("cannot verify owned Redis PID")
              token = secrets.token_hex(24)
              assert redis("SET", "rex-profile-token", token) == "OK"
              output = args.output/f"{stem}.json"
              env = dict(os.environ, GOMAXPROCS="2", LOG_LEVEL="error",
                  REX_PROFILE_ADDR=f"127.0.0.1:{port}", REX_PROFILE_TOKEN=token,
                  REX_PROFILE_EVENTS=str(args.events), REX_PROFILE_OUTPUT=str(output),
                  REX_PROFILE_SCENARIO=scenario)
              invocation = [str(binary), "-test.run=^TestDurableProfile$", "-test.count=1", "-test.timeout=6m"]
              if args.cpu_profile:
                invocation.append(f"-test.cpuprofile={args.output / f'cpu-{stem}.pprof'}")
              with (args.output/f"test-{stem}.log").open("w") as testlog:
                subprocess.run(invocation, cwd=ROOT, env=env, stdout=testlog,
                               stderr=subprocess.STDOUT, check=True, timeout=370)
              (args.output/f"redis-info-{stem}.txt").write_text(redis("INFO"))
            finally:
              proc.terminate()
              try:
                proc.wait(timeout=10)
              except subprocess.TimeoutExpired:
                proc.kill(); proc.wait()
        results = json.loads(output.read_text())
        if len(results) != 1 or results[0]["name"] != scenario:
            raise RuntimeError(f"unexpected result for {scenario}: {results}")
        for result in results:
            row = {k:v for k,v in result.items() if k != "samples"}
            row["run"] = run
            drained = sum(result["partition_events"])
            if drained != args.events or len(result["samples"]) != drained:
                raise RuntimeError(
                    f"scenario {scenario} configured {args.events}, reported {drained}, "
                    f"and sampled {len(result['samples'])} events")
            row["events_per_second"] = drained/(result["elapsed_ns"]/1e9)
            row["service"] = percentiles([s["service_ns"] for s in result["samples"]])
            row["end_to_end"] = percentiles([s["end_to_end_ns"] for s in result["samples"]])
            row["partitions"] = []
            for p, count in enumerate(result["partition_events"]):
                samples = [s for s in result["samples"] if s["partition"] == p]
                if len(samples) != count:
                    raise RuntimeError(
                        f"scenario {scenario} partition {p} reported {count} "
                        f"but sampled {len(samples)} events")
                service_ns = sum(s["service_ns"] for s in samples)
                row["partitions"].append({"partition": p, "events": count,
                    "events_per_second_over_full_drain": count/(result["elapsed_ns"]/1e9),
                    "service_events_per_second": count/(service_ns/1e9) if service_ns else 0.0,
                    "service": percentiles([s["service_ns"] for s in samples]),
                    "end_to_end": percentiles([s["end_to_end_ns"] for s in samples])})
            summary.append(row)
        print(f"run {run+1}/{args.runs} {scenario} validated", flush=True)
    (args.output/"summary.json").write_text(json.dumps(summary, indent=2)+"\n")
    if not args.cpu_profile:
        binary.unlink()


if __name__ == "__main__":
    main()
