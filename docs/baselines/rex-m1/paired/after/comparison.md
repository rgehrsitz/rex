# REX performance comparison

Matched fixtures, artifacts, action counts, sampling budgets, and environment fields were verified.

| Mode / logging | Fixture | Before p50 µs | After p50 µs | Speedup | GET/batch before → after | Investigation flags |
| --- | --- | ---: | ---: | ---: | --- | --- |
| memory / disabled-discard | missing-half | 8008.92 | 64.58 | 124.01× | 50 → 0 | None |
| memory / disabled-discard | sparse-100 | 10.83 | 7.50 | 1.44× | 10 → 0 | None |
| memory / disabled-discard | sparse-1000 | 38.92 | 5.92 | 6.58× | 10 → 0 | None |
| memory / disabled-discard | sparse-10000 | 331.79 | 6.04 | 54.91× | 10 → 0 | None |
| redis / disabled-discard | actions-8 | 17746.42 | 12014.75 | 1.48× | 80 → 0 | None |
| redis / disabled-discard | sparse-1000 | 2309.50 | 1548.79 | 1.49× | 10 → 0 | None |
| redis / disabled-discard | unique-100 | 22671.17 | 15054.71 | 1.51× | 100 → 0 | None |

Speedup uses medians of per-run p50 batch latencies; it is not a daemon capacity claim.
Heap-before values in comparison.json include program/index memory, not just evaluation allocations.
