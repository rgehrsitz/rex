# REX performance comparison

Matched fixtures, artifacts, action counts, sampling budgets, and environment fields were verified.

| Mode / logging | Fixture | Before p50 µs | After p50 µs | Speedup | GET/batch before → after | Investigation flags |
| --- | --- | ---: | ---: | ---: | --- | --- |
| memory / disabled-discard | actions-8 | 48.17 | 13.79 | 3.49× | 80 → 0 | None |
| memory / disabled-discard | batch-8 | 365.54 | 100.58 | 3.63× | 80 → 0 | None |
| memory / disabled-discard | dense-1000 | 3165.92 | 604.92 | 5.23× | 1000 → 0 | None |
| memory / disabled-discard | dependencies-32 | 67.83 | 34.75 | 1.95× | 10 → 0 | None |
| memory / disabled-discard | match-half | 39.25 | 5.67 | 6.93× | 5 → 0 | None |
| memory / disabled-discard | match-none | 36.17 | 2.38 | 15.23× | 0 → 0 | None |
| memory / disabled-discard | missing-half | 7917.88 | 66.67 | 118.77× | 50 → 0 | None |
| memory / disabled-discard | shared-100 | 377.62 | 59.17 | 6.38× | 100 → 0 | None |
| memory / disabled-discard | sparse-100 | 10.83 | 7.54 | 1.44× | 10 → 0 | None |
| memory / disabled-discard | sparse-1000 | 38.92 | 5.75 | 6.77× | 10 → 0 | None |
| memory / disabled-discard | sparse-10000 | 332.75 | 5.96 | 55.85× | 10 → 0 | None |
| memory / disabled-discard | unique-100 | 403.21 | 84.46 | 4.77× | 100 → 0 | None |
| memory / info-discard | shared-100 | 475.62 | 156.83 | 3.03× | 100 → 0 | None |
| redis / disabled-discard | actions-8 | 18167.75 | 12028.33 | 1.51× | 80 → 0 | None |
| redis / disabled-discard | batch-8 | 19011.04 | 12676.12 | 1.50× | 80 → 0 | None |
| redis / disabled-discard | dense-1000 | 228114.33 | 150842.96 | 1.51× | 1000 → 0 | None |
| redis / disabled-discard | dependencies-32 | 2425.42 | 1611.12 | 1.51× | 10 → 0 | None |
| redis / disabled-discard | match-half | 125.08 | 92.33 | 1.35× | 5 → 0 | None |
| redis / disabled-discard | match-none | 114.46 | 78.42 | 1.46× | 0 → 0 | None |
| redis / disabled-discard | missing-half | 19175.46 | 7718.12 | 2.48× | 50 → 0 | None |
| redis / disabled-discard | shared-100 | 22681.21 | 15055.58 | 1.51× | 100 → 0 | None |
| redis / disabled-discard | sparse-100 | 2285.12 | 1668.67 | 1.37× | 10 → 0 | None |
| redis / disabled-discard | sparse-1000 | 2327.96 | 1650.75 | 1.41× | 10 → 0 | None |
| redis / disabled-discard | sparse-10000 | 2672.04 | 1652.58 | 1.62× | 10 → 0 | None |
| redis / disabled-discard | unique-100 | 22916.62 | 15130.67 | 1.51× | 100 → 0 | None |

Speedup uses medians of per-run p50 batch latencies; it is not a daemon capacity claim.
Heap-before values in comparison.json include program/index memory, not just evaluation allocations.
