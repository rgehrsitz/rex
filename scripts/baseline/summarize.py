#!/usr/bin/env python3
"""Summarize raw REX baseline samples and derive provisional investigation limits.

Usage: python3 scripts/baseline/summarize.py RESULTS_DIRECTORY
Writes summary.json and summary.md beside the raw samples.
"""

import json
from pathlib import Path
import statistics
import sys


def summarize(directory):
    groups = {}
    for path in sorted(directory.glob("*.jsonl")):
        for line in path.read_text().splitlines():
            sample = json.loads(line)
            key = (sample["mode"], sample["logging"], sample["fixture"]["name"], sample["churn"])
            groups.setdefault(key, []).append(sample)
    if not groups:
        raise ValueError("no samples found")
    summaries = []
    for key, samples in sorted(groups.items()):
        if len(samples) < 3:
            raise ValueError(f"insufficient repetitions for {key}")
        if len({s["run"] for s in samples}) != len(samples):
            raise ValueError(f"duplicate runs for {key}")
        for invariant in ("source_id", "rules_sha256", "bytecode_sha256", "events", "warmup", "gomaxprocs", "go", "os", "arch"):
            if len({s[invariant] for s in samples}) != 1:
                raise ValueError(f"inconsistent {invariant} for {key}")
        record = {"mode":key[0], "logging":key[1], "fixture":key[2], "churn":key[3],
                  "runs":len(samples), "events_per_run":samples[0]["events"], "metrics":{}}
        for metric in ("p50_ns", "p95_ns", "p99_ns", "events_per_second", "allocs_per_event", "bytes_per_event", "retained_delta_bytes", "facts_retained"):
            values = [s[metric] for s in samples]
            low, high = min(values), max(values)
            stats = {"median":statistics.median(values), "min":low, "max":high}
            # Deliberately provisional: one observed range above the worst run.
            # Tail latency and heap deltas remain descriptive, not CI thresholds.
            if metric in ("p50_ns", "p95_ns", "allocs_per_event", "bytes_per_event"):
                stats["investigate_above"] = high + (high - low)
            record["metrics"][metric] = stats
        record["calls_per_event"] = {}
        for command in samples[0]["store_calls"]:
            values = [s["store_calls"][command] / s["events"] for s in samples]
            if len(set(values)) != 1:
                raise ValueError(f"unstable measured work for {key}: {command}")
            record["calls_per_event"][command] = values[0]
        if key[0] == "redis":
            for s in samples:
                calls = s["store_calls"]
                expected = {"mget":calls["mget"], "get":calls["get"], "set":calls["set"]+calls["set_publish"], "publish":calls["set_publish"]}
                if any(s["redis_commands"].get(k,0) != v for k,v in expected.items()):
                    raise ValueError(f"Redis wire command counts disagree with store calls for {key}")
        summaries.append(record)
    (directory / "summary.json").write_text(json.dumps(summaries, indent=2) + "\n")
    lines = ["# REX-M0 measured results", "", "Medians across independent runs; latency is per synchronous API batch.", "",
             "| Mode / logging | Fixture | Churn keys | p50 µs | p95 µs | Batches/s | Allocs/batch | Bytes/batch | Actions/batch |", "| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |"]
    for s in summaries:
        m = s["metrics"]
        lines.append(f'| {s["mode"]} / {s["logging"]} | {s["fixture"]} | {s["churn"]} | '
                     f'{m["p50_ns"]["median"]/1000:.2f} | {m["p95_ns"]["median"]/1000:.2f} | '
                     f'{m["events_per_second"]["median"]:.0f} | {m["allocs_per_event"]["median"]:.2f} | '
                     f'{m["bytes_per_event"]["median"]:.1f} | {s["calls_per_event"]["set_publish"]:.0f} |')
    lines.extend(["", "See `summary.json` for min/max ranges, retained-state measurements, and provisional investigation limits.",
                  "Limits are baseline maximum plus the observed range across repetitions. Compare the median of an equally sized candidate run set on the same environment and fixture.",
                  "These are local review triggers, not portable CI gates or production SLOs. p99 from short Redis runs is exploratory.", ""])
    (directory / "summary.md").write_text("\n".join(lines))
    print(f"Summarized {sum(len(v) for v in groups.values())} runs in {len(groups)} groups")


if __name__ == "__main__":
    summarize(Path(sys.argv[1]))
