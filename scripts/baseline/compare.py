#!/usr/bin/env python3
"""Compare equivalent REX measurements; write comparison.json/md in candidate dir.

Usage: python3 scripts/baseline/compare.py BASELINE_DIRECTORY CANDIDATE_DIRECTORY
Rejects changes to fixtures, artifacts, sampling, or semantic work. Investigation
flags use M0's observed-range heuristic, not a portable CI performance guarantee.
"""

import json
from pathlib import Path
import statistics
import sys


def groups(directory):
    result = {}
    for path in sorted(directory.glob("*.jsonl")):
        for line in path.read_text().splitlines():
            r = json.loads(line)
            key = (r["mode"], r["logging"], r["fixture"]["name"], r["churn"])
            result.setdefault(key, []).append(r)
    return result


def compare(before_dir, after_dir):
    before, after = groups(before_dir), groups(after_dir)
    if not after:
        raise ValueError("no candidate samples")
    records = []
    for key, new in sorted(after.items()):
        old = before[key]
        if len(old) != len(new) or len(new) < 3:
            raise ValueError(f"need equal repetition counts >=3 for {key}")
        for sample in (old, new):
            if len({r["run"] for r in sample}) != len(sample):
                raise ValueError(f"duplicate repetitions for {key}")
        for field in ("fixture", "rules_sha256", "bytecode_sha256", "events", "warmup", "gomaxprocs", "go", "os", "arch", "cpus", "facts_retained"):
            if any(r[field] != old[0][field] for r in old + new):
                raise ValueError(f"changed {field} for {key}")
        for field in ("mget", "dependency_keys", "set", "set_publish"):
            if any(r["store_calls"][field] != old[0]["store_calls"][field] for r in old + new):
                raise ValueError(f"changed semantic/store work {field} for {key}")
        for r in old + new:
            if r["mode"] == "redis":
                c = r["store_calls"]
                expected = {"get":c["get"], "mget":c["mget"], "set":c["set"]+c["set_publish"], "publish":c["set_publish"]}
                if any(r["redis_commands"].get(k, 0) != v for k, v in expected.items()):
                    raise ValueError(f"Redis command mismatch for {key}")
        record = {"mode":key[0], "logging":key[1], "fixture":key[2], "runs":len(new), "metrics":{}}
        for field in ("p50_ns", "p95_ns", "events_per_second", "allocs_per_event", "bytes_per_event", "heap_before_bytes"):
            a, b = [r[field] for r in old], [r[field] for r in new]
            am, bm = statistics.median(a), statistics.median(b)
            metric = {"before_median":am, "after_median":bm, "after_over_before":bm/am if am else None}
            if field in ("p50_ns", "p95_ns", "allocs_per_event", "bytes_per_event"):
                metric["investigate_above"] = max(a) + (max(a) - min(a))
                metric["investigate"] = bm > metric["investigate_above"]
            record["metrics"][field] = metric
        record["get_per_batch"] = {"before":statistics.median(r["store_calls"]["get"]/r["events"] for r in old),
                                    "after":statistics.median(r["store_calls"]["get"]/r["events"] for r in new)}
        records.append(record)
    (after_dir / "comparison.json").write_text(json.dumps(records, indent=2) + "\n")
    lines = ["# REX performance comparison", "", "Matched fixtures, artifacts, action counts, sampling budgets, and environment fields were verified.", "",
             "| Mode / logging | Fixture | Before p50 µs | After p50 µs | Speedup | GET/batch before → after | Investigation flags |",
             "| --- | --- | ---: | ---: | ---: | --- | --- |"]
    flagged = 0
    for r in records:
        m = r["metrics"]["p50_ns"]
        flags = [k for k, v in r["metrics"].items() if v.get("investigate")]
        flagged += len(flags)
        lines.append(f'| {r["mode"]} / {r["logging"]} | {r["fixture"]} | {m["before_median"]/1000:.2f} | '
                     f'{m["after_median"]/1000:.2f} | {m["before_median"]/m["after_median"]:.2f}× | '
                     f'{r["get_per_batch"]["before"]:g} → {r["get_per_batch"]["after"]:g} | {", ".join(flags) or "None"} |')
    lines.extend(["", "Speedup uses medians of per-run p50 batch latencies; it is not a daemon capacity claim.",
                  "Heap-before values in comparison.json include program/index memory, not just evaluation allocations.", ""])
    (after_dir / "comparison.md").write_text("\n".join(lines))
    print(f"Compared {len(records)} groups; {flagged} provisional investigation flags")


if __name__ == "__main__":
    compare(Path(sys.argv[1]), Path(sys.argv[2]))
