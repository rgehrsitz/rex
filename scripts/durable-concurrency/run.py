#!/usr/bin/env python3
"""Own disposable Redis instances and retain repeatable M8.8 evidence."""
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
ALL_SCENARIOS = ("sparse-balanced", "dense-balanced", "sparse-skew",
                 "completion-retry", "owner-loss")
ALL_WORKERS = (1, 2, 4)
PERFORMANCE_SCENARIOS = ALL_SCENARIOS[:3]
MEASUREMENT_SOURCES = (
    ROOT / "pkg/runtime/durable_concurrency_profile_test.go",
    Path(__file__).resolve(),
)


def command(args, **kwargs):
    return subprocess.check_output(args, cwd=ROOT, text=True, **kwargs).strip()


def executable_path(value, label):
    found = shutil.which(str(value))
    path = Path(found).absolute() if found else Path(value).expanduser().absolute()
    if not path.is_file() or not os.access(path, os.X_OK):
        raise ValueError(f"{label} is not executable: {path}")
    return path


def selected_values(raw, allowed, label, convert=str):
    try:
        values = tuple(convert(value.strip()) for value in raw.split(",") if value.strip())
    except ValueError as err:
        raise ValueError(f"invalid {label}: {raw}") from err
    if not values or len(values) != len(set(values)) or any(value not in allowed for value in values):
        raise ValueError(f"{label} must be unique values from {','.join(map(str, allowed))}")
    return values


def percentiles(values):
    if not values:
        return {f"p{p}_ms": None for p in (50, 95, 99)}
    ordered = sorted(values)
    return {f"p{p}_ms": ordered[max(0, math.ceil(len(ordered)*p/100)-1)]/1e6
            for p in (50, 95, 99)}


def start_redis(server, cli, output, stem):
    directory = tempfile.TemporaryDirectory(prefix="rex-m88-")
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    log = (output/f"redis-{stem}.log").open("w")
    proc = subprocess.Popen([str(server), "--bind", "127.0.0.1", "--port", str(port),
        "--save", "", "--appendonly", "no", "--dir", directory.name], stdout=log, stderr=log)

    def redis(*words):
        return command([str(cli), "-h", "127.0.0.1", "-p", str(port), *words],
                       stderr=subprocess.DEVNULL)

    try:
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
    except Exception:
        proc.terminate()
        proc.wait(timeout=10)
        log.close()
        directory.cleanup()
        raise
    return directory, log, proc, port, redis


def stop_redis(directory, log, proc):
    proc.terminate()
    try:
        proc.wait(timeout=10)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait()
    log.close()
    directory.cleanup()


def summarize(result, run, comparable, load_average):
    row = {key: value for key, value in result.items() if key != "samples"}
    row["run"] = run
    row["comparable"] = comparable
    row["host_load_average"] = load_average
    drained = sum(result["partition_events"])
    samples = result["samples"]
    if drained <= 0 or len(samples) != drained:
        raise RuntimeError(
            f"{result['name']} with {result['workers']} workers reported {drained} "
            f"and sampled {len(samples)} events")
    sequences = {}
    for sample in samples:
        sequences.setdefault(sample["partition"], []).append(sample["sequence"])
    for partition, count in enumerate(result["partition_events"]):
        if sequences.get(partition, []) != list(range(count)):
            raise RuntimeError(f"partition {partition} sequence evidence is incomplete or unordered")
    row["events_per_second"] = drained/(result["elapsed_ns"]/1e9)
    row["redis_cpu_utilization"] = result["drain_redis_cpu_seconds"]/(result["elapsed_ns"]/1e9)
    row["service"] = percentiles([sample["service_ns"] for sample in samples])
    row["backlog_completion"] = percentiles(
        [sample["backlog_completion_ns"] for sample in samples])
    row["partitions"] = []
    for partition, count in enumerate(result["partition_events"]):
        selected = [sample for sample in samples if sample["partition"] == partition]
        if len(selected) != count:
            raise RuntimeError(f"partition {partition} reported {count} but sampled {len(selected)}")
        service_ns = sum(sample["service_ns"] for sample in selected)
        row["partitions"].append({
            "partition": partition,
            "events": count,
            "events_per_second_over_full_drain": count/(result["elapsed_ns"]/1e9),
            "service_events_per_second": count/(service_ns/1e9) if service_ns else 0.0,
            "service": percentiles([sample["service_ns"] for sample in selected]),
            "backlog_completion": percentiles(
                [sample["backlog_completion_ns"] for sample in selected]),
        })
    return row


def host_cpu_metadata():
    details = {"logical_cpus": os.cpu_count()}
    if platform.system() == "Darwin":
        for key, field in (("hw.perflevel0.physicalcpu", "performance_cores"),
                           ("hw.perflevel1.physicalcpu", "efficiency_cores")):
            try:
                details[field] = int(command(["sysctl", "-n", key]))
            except (subprocess.CalledProcessError, ValueError):
                details[field] = None
    return details


def evaluate_gates(summary, workers, scenarios, comparable):
    run_count = len({row["run"] for row in summary})
    required = (run_count >= 5 and {1, 2}.issubset(workers)
                and set(PERFORMANCE_SCENARIOS).issubset(scenarios))
    if not comparable or not required:
        return {"status": "not_evaluated",
                "reason": "requires five comparable 1/2-worker runs for all performance scenarios"}
    gates = {"definition": {
        "baseline_max_throughput_cv": 0.20,
        "minimum_two_worker_median_paired_speedup": 1.50,
        "latency_limit": "two-worker service p99 maximum must not exceed one-worker maximum",
        "skew_limit": "two-worker hot-partition service p99 maximum must not exceed one-worker maximum",
        "commands": "non-fault commands per event must exactly match the one-worker case",
    }, "balanced": {}, "skew": {}, "commands": {}}
    unstable = False
    failed = False
    for scenario in PERFORMANCE_SCENARIOS[:2]:
        baseline = [row for row in summary if row["name"] == scenario and row["workers"] == 1]
        experimental = [row for row in summary if row["name"] == scenario and row["workers"] == 2]
        rates = [row["events_per_second"] for row in baseline]
        cv = statistics.pstdev(rates)/statistics.fmean(rates) if len(rates) > 1 else 0.0
        speedup = statistics.median(row["speedup_vs_one"] for row in experimental)
        baseline_p99_max = max(row["service"]["p99_ms"] for row in baseline)
        experimental_p99_max = max(row["service"]["p99_ms"] for row in experimental)
        stable = cv <= gates["definition"]["baseline_max_throughput_cv"]
        speedup_pass = speedup >= gates["definition"]["minimum_two_worker_median_paired_speedup"]
        latency_pass = experimental_p99_max <= baseline_p99_max
        gates["balanced"][scenario] = {
            "baseline_throughput_cv": cv,
            "stable": stable,
            "two_worker_median_paired_speedup": speedup,
            "speedup_pass": speedup_pass,
            "one_worker_service_p99_max_ms": baseline_p99_max,
            "two_worker_service_p99_max_ms": experimental_p99_max,
            "latency_pass": latency_pass,
        }
        unstable = unstable or not stable
        failed = failed or not speedup_pass or not latency_pass

    baseline_skew = [row for row in summary if row["name"] == "sparse-skew" and row["workers"] == 1]
    worker_two_skew = [row for row in summary if row["name"] == "sparse-skew" and row["workers"] == 2]
    baseline_hot_max = max(row["partitions"][0]["service"]["p99_ms"] for row in baseline_skew)
    worker_two_hot_max = max(row["partitions"][0]["service"]["p99_ms"] for row in worker_two_skew)
    skew_pass = worker_two_hot_max <= baseline_hot_max
    gates["skew"] = {"one_worker_hot_service_p99_max_ms": baseline_hot_max,
                     "two_worker_hot_service_p99_max_ms": worker_two_hot_max,
                     "latency_pass": skew_pass,
                     "expected_speedup_ceiling": 1/0.9}
    failed = failed or not skew_pass

    for scenario in PERFORMANCE_SCENARIOS:
        baseline_commands = {
            row["drain_redis_commands"]/sum(row["partition_events"])
            for row in summary if row["name"] == scenario and row["workers"] == 1
        }
        all_commands = {
            row["drain_redis_commands"]/sum(row["partition_events"])
            for row in summary if row["name"] == scenario
        }
        matches = len(baseline_commands) == 1 and all_commands == baseline_commands
        gates["commands"][scenario] = {"commands_per_event": sorted(all_commands),
                                         "pass": matches}
        failed = failed or not matches
    gates["status"] = "inconclusive" if unstable else ("fail" if failed else "pass")
    if unstable:
        gates["reason"] = "one-worker throughput varied beyond the stability limit"
    return gates


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--redis-server", required=True)
    parser.add_argument("--redis-cli", type=Path,
                        help="redis-cli path; defaults to the redis-server directory")
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--runs", type=int, default=5)
    parser.add_argument("--events", type=int, default=1000)
    parser.add_argument("--workers", default="1,2,4")
    parser.add_argument("--scenarios", default=",".join(ALL_SCENARIOS))
    parser.add_argument("--race", action="store_true", help="correctness run; timing is not comparable")
    parser.add_argument("--cpu-profile", action="store_true", help="retain Go CPU profiles")
    parser.add_argument("--seed", type=int, default=8808, help="saved deterministic case-order seed")
    args = parser.parse_args()
    if not 1 <= args.runs <= 10 or not 100 <= args.events <= 10000 or args.events % 100:
        parser.error("runs must be 1..10; events must be 100..10000 and a multiple of 100")
    try:
        workers = selected_values(args.workers, ALL_WORKERS, "workers", int)
        scenarios = selected_values(args.scenarios, ALL_SCENARIOS, "scenarios")
        server = executable_path(args.redis_server, "redis-server")
        cli = executable_path(args.redis_cli or server.with_name("redis-cli"), "redis-cli")
    except ValueError as err:
        parser.error(str(err))
    args.output = args.output.resolve()
    args.output.mkdir(parents=True, exist_ok=False)

    changed = set(command(["git", "diff", "--name-only", "HEAD", "--", "*.go", "go.mod", "go.sum"]).splitlines())
    untracked = set(command(["git", "ls-files", "--others", "--exclude-standard", "--", "*.go"]).splitlines())
    allowed = {str(path.relative_to(ROOT)) for path in MEASUREMENT_SOURCES if path.suffix == ".go"}
    unexpected = (changed | untracked) - allowed
    if unexpected:
        raise RuntimeError(f"production Go/module sources must match HEAD; changed: {sorted(unexpected)}")

    gomaxprocs = os.environ.get("GOMAXPROCS", "4")
    try:
        if int(gomaxprocs) < max(workers):
            raise ValueError
    except ValueError:
        parser.error("GOMAXPROCS must be an integer at least as large as the largest worker count")
    comparable = not args.race and not args.cpu_profile
    metadata = {
        "revision": command(["git", "rev-parse", "HEAD"]),
        "status": command(["git", "status", "--short"]),
        "go": command(["go", "version"]),
        "platform": platform.platform(),
        "redis": command([str(server), "--version"]),
        "redis_cli": command([str(cli), "--version"]),
        "gomaxprocs": gomaxprocs,
        "host_cpu": host_cpu_metadata(),
        "events_per_scenario": args.events,
        "runs": args.runs,
        "workers": workers,
        "scenarios": scenarios,
        "race": args.race,
        "cpu_profile": args.cpu_profile,
        "comparable": comparable,
        "case_order_seed": args.seed,
        "persistence": "disabled: save empty, appendonly no",
        "network": "loopback TCP; one Redis process shared by all partitions in a case",
        "scheduler": "one serial goroutine and distinct store, engine, queue, and lease per partition",
        "scope": "processor-loop experiment; no lease renewers, daemon supervisor, reload, routing, migration, or temporal artifacts",
        "sha256": {str(path.relative_to(ROOT)): hashlib.sha256(path.read_bytes()).hexdigest()
                   for path in MEASUREMENT_SOURCES},
    }
    (args.output/"metadata.json").write_text(json.dumps(metadata, indent=2)+"\n")
    binary = args.output/"runtime.test"
    build = ["go", "test", "-c", "-o", str(binary)]
    if args.race:
        build.append("-race")
    subprocess.run(build+["./pkg/runtime"], cwd=ROOT, check=True)

    summary = []
    for run in range(args.runs):
        cases = [(scenario, worker_count) for scenario in scenarios for worker_count in workers]
        random.Random(args.seed+run).shuffle(cases)
        for scenario, worker_count in cases:
            stem = f"run-{run}-{scenario}-w{worker_count}"
            load_average = os.getloadavg()
            directory, log, proc, port, redis = start_redis(server, cli, args.output, stem)
            try:
                token = secrets.token_hex(24)
                if redis("SET", "rex-concurrency-token", token) != "OK":
                    raise RuntimeError("cannot establish Redis ownership token")
                output = args.output/f"{stem}.json"
                env = dict(os.environ, GOMAXPROCS=gomaxprocs, LOG_LEVEL="error",
                    REX_CONCURRENCY_ADDR=f"127.0.0.1:{port}", REX_CONCURRENCY_TOKEN=token,
                    REX_CONCURRENCY_EVENTS=str(args.events), REX_CONCURRENCY_WORKERS=str(worker_count),
                    REX_CONCURRENCY_SCENARIO=scenario, REX_CONCURRENCY_OUTPUT=str(output))
                invocation = [str(binary), "-test.run=^TestDurableConcurrencyProfile$",
                              "-test.count=1", "-test.timeout=6m"]
                if args.cpu_profile:
                    invocation.append(f"-test.cpuprofile={args.output / f'cpu-{stem}.pprof'}")
                with (args.output/f"test-{stem}.log").open("w") as testlog:
                    subprocess.run(invocation, cwd=ROOT, env=env, stdout=testlog,
                                   stderr=subprocess.STDOUT, check=True, timeout=370)
                (args.output/f"redis-info-{stem}.txt").write_text(redis("INFO"))
            finally:
                stop_redis(directory, log, proc)
            results = json.loads(output.read_text())
            if len(results) != 1 or results[0]["name"] != scenario or results[0]["workers"] != worker_count:
                raise RuntimeError(f"unexpected result for {scenario}/w{worker_count}: {results}")
            row = summarize(results[0], run, comparable, load_average)
            if sum(results[0]["partition_events"]) != args.events:
                raise RuntimeError(f"configured {args.events} but drained a different event count")
            summary.append(row)
            (args.output/"summary.partial.json").write_text(json.dumps(summary, indent=2)+"\n")
            print(f"run {run+1}/{args.runs} {scenario} w{worker_count} validated", flush=True)

    by_case = {(row["run"], row["name"], row["workers"]): row for row in summary}
    if 1 in workers and comparable:
        for row in summary:
            if row["name"] in PERFORMANCE_SCENARIOS:
                baseline = by_case[(row["run"], row["name"], 1)]["events_per_second"]
                row["speedup_vs_one"] = row["events_per_second"]/baseline
                row["parallel_efficiency"] = row["speedup_vs_one"]/row["workers"]
    (args.output/"summary.json").write_text(json.dumps(summary, indent=2)+"\n")
    (args.output/"gates.json").write_text(
        json.dumps(evaluate_gates(summary, workers, scenarios, comparable), indent=2)+"\n")
    (args.output/"summary.partial.json").unlink()
    if not args.cpu_profile:
        binary.unlink()


if __name__ == "__main__":
    main()
