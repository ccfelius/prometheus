# ALP encoding improvements

The default encoding improvements reduce wasted selection work, allocations,
and scalar packing. Histogram v4 is implemented as an explicit experimental
conversion API because smaller output can require additional encoding work.
Configured histogram writers now consistently emit v3; v1 and v2 remain readable.

Measurements use identical Go 1.27.1 SIMD toolchains on Apple M5 Pro, ARM64.
The final comparison alternates frozen before/after binaries across 29 shuffled
scenarios, with six repetitions per operation. It contains 1,368
observations and 228 medians. Final measurement began
`2026-10-01T09:41:19.570970+00:00`. Earlier exploratory runs began September 30 and are
preserved separately. No AMD64 runtime performance is claimed.
The baseline revision is `a0de5333b`; the measured final source is
`c60180dc3768b18dd229e25cb6f24f87161d0b0a`. Binary hashes and embedded build information are recorded
in the metadata. The final binary was built immediately before committing that
source, so its embedded VCS revision names the preceding commit plus local edits.

Every paired existing-format case retained the same encoded bytes per sample.
This is fixture coverage, not a guarantee that bounded parameter search always
selects the same representation on other data.

## Float encoding

120 samples, regular timestamps, no start timestamps. CPU cost is process
user plus system nanoseconds per sample, numerically equivalent to milliseconds
per million samples. Values are medians. The baseline is the previous ALP
implementation, not XOR2; unchanged XOR2 controls are in the full data.

| Pattern | Encode µs/chunk before → after | Speedup | CPU ns/sample | Allocated B/chunk | Allocations/chunk | Encoded B/sample |
| --- | --- | --- | --- | --- | --- | --- |
| constant | 1.313 → 1.054 | 1.25× | 10.62 → 8.53 | 7,600 → 4,552 | 14 → 7 | 0.333 → 0.333 |
| decimal2 | 2.841 → 2.466 | 1.15× | 23.25 → 20.27 | 7,600 → 4,552 | 14 → 7 | 1.308 → 1.308 |
| decimal6 | 6.993 → 6.644 | 1.05× | 57.84 → 55.14 | 7,600 → 4,552 | 14 → 7 | 1.392 → 1.392 |
| computed | 9.306 → 4.410 | 2.11× | 77.15 → 36.43 | 7,600 → 4,552 | 14 → 7 | 6.742 → 6.742 |
| random-bits | 12.159 → 7.309 | 1.66× | 100.80 → 60.66 | 9,008 → 4,552 | 15 → 7 | 8.267 → 8.267 |

## Decoding existing formats

These timings include materializing all 120 samples and reuse the iterator and
histogram destination. The table compares the previous and final ALP code for
the same format. Float cases omit start timestamps; histogram cases include them.

| Input | Buckets | Decode µs/chunk | CPU ns/sample | Speedup |
| --- | --- | --- | --- | --- |
| float/decimal2 | 0 | 0.379 → 0.382 | 3.16 → 3.19 | 0.99× |
| float/computed | 0 | 0.559 → 0.561 | 4.66 → 4.68 | 1.00× |
| float/random-bits | 0 | 0.307 → 0.308 | 2.56 → 2.56 | 1.00× |
| integer-histogram/smooth | 8 | 4.027 → 4.163 | 33.56 → 34.70 | 0.97× |
| integer-histogram/smooth | 128 | 25.820 → 26.174 | 215.15 → 218.15 | 0.99× |
| integer-histogram/bursty | 128 | 28.925 → 29.299 | 241.10 → 244.20 | 0.99× |
| float-histogram/smooth | 128 | 15.960 → 16.093 | 133.05 → 134.15 | 0.99× |
| float-histogram/noisy-fractional | 128 | 35.798 → 35.575 | 298.35 → 296.50 | 1.01× |

## Incremental histogram conversion

The source is already encoded. A reusable encoder processes repeated chunks
from one homogeneous series. Adaptive rejection results therefore include
amortized retry/backoff costs; they are not the latency of a cold first decision.
One observation is a whole histogram. Forced conversion in this table uses v3.
For forced conversion, 100% accepted only means a candidate was returned; it
does not mean it was smaller. Adaptive conversion enforces the savings policy.

| Input | Buckets | Policy | Convert µs/chunk | Speedup | CPU ns/snapshot | Allocated B/chunk | Allocations | Accepted after |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| integer-histogram/smooth | 8 | forced | 17.34 → 16.88 | 1.03× | 144.4 → 140.6 | 2,024 → 1,392 | 16 → 11 | 100.0% |
| integer-histogram/smooth | 8 | adaptive | 17.47 → 17.02 | 1.03× | 145.5 → 141.8 | 2,024 → 1,392 | 16 → 11 | 100.0% |
| integer-histogram/bursty | 128 | forced | 184.19 → 181.91 | 1.01× | 1534.0 → 1515.0 | 20,590 → 19,986 | 16 → 11 | 100.0% |
| integer-histogram/bursty | 128 | adaptive | 184.00 → 6.87 | 26.77× | 1532.5 → 57.0 | 20,589 → 3,504 | 16 → 9 | 0.0% |
| float-histogram/fractional | 8 | forced | 27.95 → 26.07 | 1.07× | 232.8 → 217.1 | 4,362 → 3,793 | 16 → 11 | 100.0% |
| float-histogram/fractional | 8 | adaptive | 27.97 → 26.18 | 1.07× | 232.9 → 218.1 | 4,362 → 3,793 | 16 → 11 | 100.0% |
| float-histogram/noisy-fractional | 128 | forced | 652.35 → 429.02 | 1.52× | 5395.0 → 3561.0 | 623,656 → 222,965 | 40 → 12 | 100.0% |
| float-histogram/noisy-fractional | 128 | adaptive | 657.39 → 428.58 | 1.53× | 5425.5 → 3558.5 | 623,656 → 222,965 | 40 → 12 | 100.0% |

## Histogram v4 tradeoffs

These results compare v3 and v4 from the same final binary, including appending
through the existing mutable histogram codec and final serialization. Decode
materializes every histogram with reused output buffers. The existing codec is
the ST histogram encoding. All rows have 120 snapshots per input batch.

| Input | Buckets | Existing B/snapshot | ALP B/snapshot v3 → v4 | Encode µs v3 → v4 | Decode µs v3 → v4 |
| --- | --- | --- | --- | --- | --- |
| integer-histogram/smooth | 8 | 8.92 | 5.83 → 5.64 | 33.99 → 37.55 | 4.16 → 4.19 |
| integer-histogram/bursty | 8 | 14.06 | 20.36 → 16.27 | 48.97 → 56.80 | 4.36 → 4.16 |
| integer-histogram/bursty | 128 | 94.92 | 131.30 → 94.50 | 434.36 → 534.73 | 29.30 → 29.38 |
| float-histogram/smooth | 8 | 20.69 | 19.02 → 11.97 | 53.84 → 60.66 | 2.90 → 4.01 |
| float-histogram/smooth | 128 | 177.20 | 250.10 → 95.71 | 450.51 → 523.54 | 16.09 → 29.73 |
| float-histogram/fractional | 128 | 943.00 | 225.10 → 225.10 | 718.29 → 743.51 | 20.02 → 20.04 |
| float-histogram/noisy-fractional | 128 | 940.90 | 832.60 → 832.60 | 850.81 → 852.02 | 35.58 → 35.63 |

V4 is not automatically selected by `histograms: auto`. Its extra predictor and
exception searches cost CPU, and it is not universally smaller than the existing
histogram codec. The explicit `ALPEncoder.RecodeHistogramV4` API makes the format
available for controlled evaluation. Existing v3 output remains the default.

## What changed

1. **Histogram adaptive rejection.** `RecodeHistogramIfSmaller` owns the size
   policy used by compaction. It stops trials once committed numeric/time bytes
   exceed the allowed complete size, uses a conservative 32-snapshot prefix
   estimate when enough complete blocks exist, and retries rejected layouts
   every eighth chunk. The prefix ignores the initial predictor transient and
   requires a 25% projected overshoot. Layout/reset/sample-count changes and
   series boundaries invalidate rejection hints. Final acceptance still requires
   at least 5% savings. Sampling can miss compression opportunities.
2. **Float selection.** The initial adaptive probe returns its decimal plan to
   the full encoder. Full validation remains mandatory. A failed common-scale
   sample no longer expands to all 190 parameter pairs when none appears better
   than raw storage. RD/raw fallback remains exact. Already evaluated finalist
   pairs are skipped. Less search can change the chosen encoding or miss savings
   on workloads outside the measured fixtures.
3. **Allocation and copying.** Fresh float chunks lazily reserve one 128-sample
   pending block. Histogram conversion reserves bounded numeric capacity from
   the source size, then allocates the final owned output exactly once. Numeric
   reservation is capped at 256 KiB; retained scratch still obeys the existing
   64 KiB per-buffer limit. Returned bytes never alias reusable scratch. The
   float reservation improves common chunks but reserves more memory for very
   short active series than the old geometric-growth approach.
4. **SIMD encoding.** Packing now operates across independent lanes on NEON,
   AVX2, and AVX-512 while preserving the existing vertical byte layout. Partial
   vectors use bounded scalar tails. NEON and AVX-512 combine decimal conversion,
   min/max, and exception counting; AVX2 retains its full-range fallback and
   separate reduction. Integer reconstruction for v4 float counts is also
   vectorized, with a scalar path for strides narrower than the hardware vector.
5. **Consistent explicit writing.** New explicit ALP histogram chunks and forced
   conversion now emit v3. Existing persisted chunks retain their format on
   append resumption. Readers for v1/v2 remain available; readers predating v3
   cannot read the new configured output.
6. **Experimental v4.** Integer blocks can patch sparse large residuals and
   select delta or delta-of-delta. Bucket prediction operates on signed delta
   bits before zigzag. Float blocks can encode temporal differences after exact
   decimal conversion; initial values and subsequent differences use separate
   integer frames. Special values fall back to ordinary lossless ALP. Every
   alternative is compared with its local baseline before being selected.

An initial paired run exposed a 7–14% regression in v3 integer-histogram
decoding after adding v4 format dispatch. Hoisting the format checks outside
the per-bucket materialization loops removed that regression in a focused
rerun. The final paired results include that fix. The diagnostic run and its
statistics are preserved under [pre-hoist/](pre-hoist/).

The final run still records small statistically significant regressions: v3
integer-histogram decoding costs about 1–3.4% more, and wide smooth integer
encoding costs 2.2% more. The case with the largest percentage increase adds about
1.1 ns per snapshot (4.027 to 4.163 microseconds per 120-snapshot chunk).
These costs remain visible and are retained as the tradeoff for shared v4
support; they are not reported as improvements or dismissed as noise. Further
dispatch specialization is a possible follow-up. V4's much larger temporal
float decoding cost is an additional reason it remains explicitly opt-in.

The [format specification](../../format/alp_histograms.md) documents the complete
v4 representation, arithmetic, validation, and compatibility rules.

## Deliberately retained design choices

The mutable ST histogram appender remains responsible for layout recoding,
counter resets, staleness, and snapshots. Replacing it with a raw buffered
builder would require preserving those semantics while bounding active-series
memory. For example, 120 snapshots with 128 buckets contain 125,760 raw numeric
bytes before timestamps or layout, versus about 21,264 encoded bytes for the
smooth float fixture's existing codec. This comparison is not a memory profile,
but it illustrates why retaining all raw snapshots is not an acceptable default.
A bounded direct builder and broader semantic field grouping remain follow-up
work; this change does not claim to remove the first legacy encoding stage.

## Validation and limits

- Full scalar and ARM64 SIMD chunkenc tests passed, including all matrix fixtures.
- ALP compaction and remote-read integration tests passed.
- Race-enabled codec, TSDB, and remote-read tests passed.
- Histogram decoder fuzzing completed 705,027 executions without a failure.
- All packing widths, partial vectors, scalar byte parity, signed/unsigned
  extremes, stale NaNs, signed zero, custom layouts, and reset behavior are covered.
- New malformed-input tests cover exception ordering/ranges, frame overflow,
  temporal stride, lengths, exponent/factor bounds, and truncation.
- Linux AMD64 SIMD compilation passed. AVX2/AVX-512 runtime testing is outstanding.
- `make lint` passed with the SIMD experiment enabled.

Captured check output is in [validation/](validation/). Race, fuzz, integration,
and cross-compilation checks ran before the final loop-hoisting change; full
scalar/SIMD codec tests and lint were rerun afterward.

These are synthetic codec benchmarks on one ARM64 machine, with
`GOMAXPROCS=1`, `GOGC=100`, 150 ms per repetition and six repetitions. They do not
measure end-to-end ingestion, PromQL, disk I/O, or production workload mixes.
CPU percentage is process CPU time divided by wall time, expressed as a
percentage of one core. These serial CPU-bound loops usually saturate one core;
CPU nanoseconds per sample is the useful comparison of work consumed. Histogram
CPU costs are per complete snapshot, not per bucket. Allocation counts and bytes
are per whole chunk operation. This is not an RSS or peak-memory measurement.
The unchanged XOR2 and legacy histogram controls help assess host timing drift;
small differences should be read with the statistical comparisons.

This incremental report reruns XOR2 and existing histogram controls. The broader
[XOR/XOR2/ALP comparison](../alp-xor-xor2-20260930/report.md) includes original XOR;
original XOR was not rerun in this change's paired experiment.

[All medians](summary.csv), [machine-readable medians](summary.json),
[paired benchstat](benchstat.txt), [v3 versus v4 benchstat](v3-v4-benchstat.txt),
and [execution metadata](metadata.json) are
included. Individual process output is in [raw/](raw/). `before.txt`, `stage1.txt`,
and `after.txt` preserve the earlier exploratory runs; final conclusions use
`paired-before.txt` and `paired-after.txt` exclusively.

## Reproduction

Compile the baseline revision `a0de5333b` in a separate checkout to
`/tmp/alp-improve-before.test`, and the final implementation to
`/tmp/alp-improve-final.test`, both with:

```sh
GOTOOLCHAIN=go1.27.1 GOEXPERIMENT=simd go test -c ./tsdb/chunkenc -o /tmp/SELECTED-BINARY.test
```

From the final checkout:

```sh
python3 tsdb/docs/benchmarks/alp-encoding-improvements-20260930/run.py --before /tmp/alp-improve-before.test --after /tmp/alp-improve-final.test
python3 tsdb/docs/benchmarks/alp-encoding-improvements-20260930/analyze.py
cd tsdb/docs/benchmarks/alp-encoding-improvements-20260930
GOTOOLCHAIN=go1.27.1 go run golang.org/x/perf/cmd/benchstat@v0.0.0-20260908200009-22c9c6c9d4da paired-before.txt paired-after.txt > benchstat.txt
GOTOOLCHAIN=go1.27.1 go run golang.org/x/perf/cmd/benchstat@v0.0.0-20260908200009-22c9c6c9d4da ALPv3.txt ALPv4.txt > v3-v4-benchstat.txt
```

The runner records binary hashes and build metadata, serializes timed processes,
alternates before/after order by case, and validates six observations per row.
