#!/usr/bin/env python3
"""Run paired WATCH/script durable measurements against disposable Redis."""
import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import random
import secrets
import shutil
import socket
import statistics
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parents[2]
MODES = ("watch", "script")
SCENARIOS = ("sparse-balanced", "dense-balanced")
SOURCES = (
    ROOT / "pkg/runtime/durable_profile_test.go",
    ROOT / "pkg/store/redis_durable.go",
    ROOT / "pkg/store/redis_durable_scripts.go",
    Path(__file__).resolve(),
)


def command(args, **kwargs):
    return subprocess.check_output(args, cwd=ROOT, text=True, **kwargs).strip()


def executable(value, label):
    found = shutil.which(str(value))
    path = Path(found).absolute() if found else Path(value).expanduser().absolute()
    if not path.is_file() or not os.access(path, os.X_OK):
        raise ValueError(f"{label} is not executable: {path}")
    return path


def percentile(samples, percent):
    values = sorted(sample["service_ns"] for sample in samples)
    return values[max(0, math.ceil(len(values) * percent / 100) - 1)] / 1e6


def cv(values):
    return statistics.stdev(values) / statistics.mean(values) if len(values) > 1 else 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--redis-server", required=True)
    parser.add_argument("--redis-cli")
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--runs", type=int, default=5)
    parser.add_argument("--events", type=int, default=1000)
    parser.add_argument("--retain-raw", action="store_true")
    args = parser.parse_args()
    if not 3 <= args.runs <= 10 or not 100 <= args.events <= 10000 or args.events % 100:
        parser.error("runs must be 3..10; events must be 100..10000 and a multiple of 100")
    try:
        server = executable(args.redis_server, "redis-server")
        cli = executable(args.redis_cli or server.with_name("redis-cli"), "redis-cli")
    except ValueError as err:
        parser.error(str(err))
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    metadata = {
        "revision": command(["git", "rev-parse", "HEAD"]),
        "status": command(["git", "status", "--short"]),
        "go": command(["go", "version"]),
        "platform": platform.platform(),
        "redis": command([str(server), "--version"]),
        "events_per_case": args.events,
        "runs": args.runs,
        "modes": MODES,
        "scenarios": SCENARIOS,
        "gomaxprocs": 2,
        "network": "loopback TCP",
        "persistence": "disabled",
        "scheduler": "serial round-robin driver across four isolated partitions",
        "raw_case_files_retained": args.retain_raw,
        "sha256": {str(path.relative_to(ROOT)): hashlib.sha256(path.read_bytes()).hexdigest() for path in SOURCES},
    }
    (output / "metadata.json").write_text(json.dumps(metadata, indent=2) + "\n")
    binary = output / "runtime.test"
    subprocess.run(["go", "test", "-c", "-o", str(binary), "./pkg/runtime"], cwd=ROOT, check=True)
    order = [(run, scenario, mode) for run in range(args.runs) for scenario in SCENARIOS for mode in MODES]
    random.Random(89013).shuffle(order)
    rows = []
    for run, scenario, mode in order:
        stem = f"run-{run}-{scenario}-{mode}"
        with tempfile.TemporaryDirectory(prefix="rex-m89-") as directory:
            with socket.socket() as sock:
                sock.bind(("127.0.0.1", 0))
                port = sock.getsockname()[1]
            with (output / f"redis-{stem}.log").open("w") as log:
                proc = subprocess.Popen([str(server), "--bind", "127.0.0.1", "--port", str(port),
                    "--save", "", "--appendonly", "no", "--dir", directory], stdout=log, stderr=log)
                try:
                    def redis(*words):
                        return command([str(cli), "-h", "127.0.0.1", "-p", str(port), *words], stderr=subprocess.DEVNULL)
                    for _ in range(100):
                        if proc.poll() is not None:
                            raise RuntimeError("owned Redis exited")
                        try:
                            if f"process_id:{proc.pid}\n" in redis("INFO", "server").replace("\r", ""):
                                break
                        except subprocess.CalledProcessError:
                            pass
                        time.sleep(.05)
                    else:
                        raise RuntimeError("cannot verify owned Redis")
                    token = secrets.token_hex(24)
                    assert redis("SET", "rex-profile-token", token) == "OK"
                    raw = output / f"{stem}.json"
                    env = dict(os.environ, GOMAXPROCS="2", LOG_LEVEL="error",
                        REX_PROFILE_ADDR=f"127.0.0.1:{port}", REX_PROFILE_TOKEN=token,
                        REX_PROFILE_EVENTS=str(args.events), REX_PROFILE_OUTPUT=str(raw),
                        REX_PROFILE_SCENARIO=scenario, REX_PROFILE_TRANSACTION_MODE=mode)
                    with (output / f"test-{stem}.log").open("w") as testlog:
                        subprocess.run([str(binary), "-test.run=^TestDurableProfile$", "-test.count=1", "-test.timeout=6m"],
                            cwd=ROOT, env=env, stdout=testlog, stderr=subprocess.STDOUT, check=True, timeout=370)
                    result = json.loads(raw.read_text())[0]
                    elapsed = result["elapsed_ns"] / 1e9
                    rows.append({"run": run, "scenario": scenario, "mode": mode,
                        "events_per_second": args.events / elapsed,
                        "service_p50_ms": percentile(result["samples"], 50),
                        "service_p95_ms": percentile(result["samples"], 95),
                        "service_p99_ms": percentile(result["samples"], 99),
                        "alloc_bytes_per_event": result["drain_alloc_bytes"] / args.events,
                        "alloc_objects_per_event": result["drain_alloc_objects"] / args.events,
                        "redis_cpu_seconds_per_event": result["drain_redis_cpu_seconds"] / args.events,
                        "redis_commands_per_event": result["drain_redis_commands"] / args.events})
                finally:
                    proc.terminate()
                    try:
                        proc.wait(timeout=10)
                    except subprocess.TimeoutExpired:
                        proc.kill(); proc.wait()
        print(f"{stem} validated", flush=True)
    (output / "summary.json").write_text(json.dumps(rows, indent=2) + "\n")
    gates = {}
    for scenario in SCENARIOS:
        by_mode = {mode: [row for row in rows if row["scenario"] == scenario and row["mode"] == mode] for mode in MODES}
        watch_rate = statistics.median(row["events_per_second"] for row in by_mode["watch"])
        script_rate = statistics.median(row["events_per_second"] for row in by_mode["script"])
        threshold = 1.5 if scenario == "sparse-balanced" else 1.15
        gates[scenario] = {
            "watch_median_events_per_second": watch_rate,
            "script_median_events_per_second": script_rate,
            "throughput_ratio": script_rate / watch_rate,
            "throughput_threshold": threshold,
            "throughput_pass": script_rate / watch_rate >= threshold,
            "watch_cv": cv([row["events_per_second"] for row in by_mode["watch"]]),
            "script_cv": cv([row["events_per_second"] for row in by_mode["script"]]),
            "stability_pass": max(cv([row["events_per_second"] for row in by_mode["watch"]]), cv([row["events_per_second"] for row in by_mode["script"]])) <= .20,
            "script_p99_max_ms": max(row["service_p99_ms"] for row in by_mode["script"]),
            "watch_p99_max_ms": max(row["service_p99_ms"] for row in by_mode["watch"]),
            "latency_pass": max(row["service_p99_ms"] for row in by_mode["script"]) <= max(row["service_p99_ms"] for row in by_mode["watch"]),
        }
    gates["overall_pass"] = all(value.get("throughput_pass") and value.get("stability_pass") and value.get("latency_pass") for value in gates.values())
    (output / "gates.json").write_text(json.dumps(gates, indent=2) + "\n")
    if not args.retain_raw:
        for pattern in ("run-*.json", "redis-*.log", "test-*.log"):
            for path in output.glob(pattern):
                path.unlink()
    binary.unlink()


if __name__ == "__main__":
    main()
