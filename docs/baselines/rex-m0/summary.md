# REX-M0 measured results

Medians across independent runs; latency is per synchronous API batch.

| Mode / logging | Fixture | Churn keys | p50 µs | p95 µs | Batches/s | Allocs/batch | Bytes/batch | Actions/batch |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| memory / disabled-discard | actions-8 | 0 | 48.17 | 52.92 | 20289 | 317.00 | 8504.0 | 80 |
| memory / disabled-discard | batch-8 | 0 | 365.54 | 422.79 | 2680 | 1712.00 | 108736.3 | 80 |
| memory / disabled-discard | dense-1000 | 0 | 3165.92 | 3314.54 | 313 | 10007.00 | 464505.3 | 1000 |
| memory / disabled-discard | dependencies-32 | 0 | 67.83 | 90.83 | 14082 | 469.00 | 57144.2 | 10 |
| memory / disabled-discard | match-half | 0 | 39.25 | 42.25 | 25715 | 82.00 | 4664.0 | 5 |
| memory / disabled-discard | match-none | 0 | 36.17 | 40.79 | 27059 | 47.00 | 4104.0 | 0 |
| memory / disabled-discard | missing-half | 0 | 7917.88 | 8114.21 | 126 | 535.00 | 91336.2 | 50 |
| memory / disabled-discard | shared-100 | 0 | 377.62 | 402.96 | 2615 | 1007.00 | 46904.1 | 100 |
| memory / disabled-discard | sparse-100 | 0 | 10.83 | 11.88 | 89272 | 107.00 | 5144.0 | 10 |
| memory / disabled-discard | sparse-100 | 1 | 0.08 | 0.12 | 8713648 | 2.00 | 32.0 | 0 |
| memory / disabled-discard | sparse-100 | 1000 | 0.08 | 0.12 | 7769253 | 2.75 | 53.8 | 0 |
| memory / disabled-discard | sparse-100 | 10000 | 0.08 | 0.12 | 6643785 | 2.98 | 170.5 | 0 |
| memory / disabled-discard | sparse-1000 | 0 | 38.92 | 42.38 | 25174 | 107.00 | 5144.0 | 10 |
| memory / disabled-discard | sparse-10000 | 0 | 332.75 | 350.62 | 2970 | 107.00 | 5144.0 | 10 |
| memory / disabled-discard | unique-100 | 0 | 403.21 | 470.38 | 2425 | 1029.00 | 110632.3 | 100 |
| memory / info-discard | shared-100 | 0 | 475.62 | 491.38 | 2088 | 1007.06 | 46920.2 | 100 |
| redis / disabled-discard | actions-8 | 0 | 18167.75 | 18729.17 | 55 | 3391.24 | 130646.4 | 80 |
| redis / disabled-discard | batch-8 | 0 | 19011.04 | 19678.88 | 52 | 5424.32 | 254945.4 | 80 |
| redis / disabled-discard | dense-1000 | 0 | 228114.33 | 238251.29 | 4 | 48044.20 | 1977982.1 | 1000 |
| redis / disabled-discard | dependencies-32 | 0 | 2425.42 | 2551.29 | 415 | 1084.12 | 82372.6 | 10 |
| redis / disabled-discard | match-half | 0 | 125.08 | 2454.04 | 800 | 306.02 | 13386.6 | 5 |
| redis / disabled-discard | match-none | 0 | 114.46 | 122.42 | 8655 | 81.00 | 5264.0 | 0 |
| redis / disabled-discard | missing-half | 0 | 19175.46 | 19430.67 | 52 | 4952.52 | 273749.1 | 50 |
| redis / disabled-discard | shared-100 | 0 | 22681.21 | 23168.33 | 44 | 4841.28 | 199291.5 | 100 |
| redis / disabled-discard | sparse-100 | 0 | 2285.12 | 2342.50 | 436 | 521.02 | 21426.8 | 10 |
| redis / disabled-discard | sparse-1000 | 0 | 2327.96 | 2436.12 | 427 | 521.12 | 21433.1 | 10 |
| redis / disabled-discard | sparse-10000 | 0 | 2672.04 | 2732.58 | 374 | 521.06 | 21428.6 | 10 |
| redis / disabled-discard | unique-100 | 0 | 22916.62 | 23172.38 | 44 | 7646.64 | 378421.6 | 100 |

See `summary.json` for min/max ranges, retained-state measurements, and provisional investigation limits.
Limits are baseline maximum plus the observed range across repetitions. Compare the median of an equally sized candidate run set on the same environment and fixture.
These are local review triggers, not portable CI gates or production SLOs. p99 from short Redis runs is exploratory.
