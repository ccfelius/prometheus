# ALP optimization results

Implemented on `tsdb-alp-optimization`, based on `606f11f67` (`tsdb-alp-simd`).
This report records the completed portion of the [optimization plan](alp-optimization-plan.md).
ALP remains experimental and opt-in; default encodings are unchanged.

A separate [ALP versus XOR2 benchmark](benchmarks/alp-xor2-20260929/report.md)
compares the optimized codec with XOR2 and measures process CPU time per sample.
The [histogram comparison](benchmarks/alp-histograms-20260929/report.md) covers
integer v1/v2 and floating-count histograms against their existing codecs.
The [detailed implementation report](alp-implementation-details.md) explains
the code paths, formats, SIMD arithmetic, integration, and remaining limits.

## Changes

- Sample common decimal scales first, precompute safe integer bounds, retain the
  winning conversion, and reuse a four-byte parameter hint across blocks and
  finalized float chunks of a series. Refresh hints periodically and after poor
  results. Every accepted value still requires exact bitwise reconstruction.
- Convert decimal candidates with explicit `simd/archsimd` instructions: two
  values per NEON vector, four per AVX2 vector, or eight per AVX-512 vector.
  AVX2 uses scalar conversion for lanes outside its exact bit-construction range.
  Ordinary builds retain the scalar implementation and the same wire format.
- Sort RD prefixes once and derive each split from that sorted sequence. Compute
  packed sizes directly and clear only required unpacking padding.
- Read mutable histogram snapshots directly from copied legacy bytes. Queries
  no longer trigger ALP serialization after each append. Transcode finalized
  histograms directly from their source iterator, reusing histogram objects.
- Unpack histogram integers and restore predictors with SIMD across independent
  fields. Batch floating-count materialization and allocate only the numeric
  buffer needed by the histogram type. Constant start timestamps need no array.
- Add adaptive compaction and integer histogram version 2, described below.

The baseline decimal-encoding CPU profile attributed 78.43% of cumulative samples
to candidate analysis, including 61.27% flat in scalar number conversion, and
10.78% flat to packed-size calculation. This explains why vectorized decoding
alone did not address the original encoding cost.

Encoding scratch stays local, and hints retain no source/output buffers. Mutable
float tails retain incremental allocation: eagerly allocating 128 entries would
increase Head memory for short-lived series. Finalized float recoding can reserve
the known sample count without imposing that cost on new active series.

## Measurements

Measured on 2026-09-29, Apple M5 Pro, darwin/arm64, Go 1.27.1,
`GOEXPERIMENT=simd`. Each case ran six times with `-benchmem`. Baseline and final
codec runs used `-benchtime=200ms`; added baseline path/cold-iterator cases used
100ms, with six repetitions. The baseline checkout contained only the added
path benchmark file, with production code unchanged. No lint/test workloads ran
alongside the final benchmark process.

These are synthetic codec benchmarks. Ordinary chunk cases contain 120 samples;
histogram fixtures have eight buckets. A write includes construction, append,
and serialization. A warmed read scans and materializes the complete chunk.
The cold-read benchmark means a newly allocated iterator through its first
sample, **not a cold CPU or disk cache**. Query-after-append performs 120 appends,
each followed by iterator creation/reset and a first-sample read; it measures
that codec pattern, not a PromQL query or lock contention.

Selected `benchstat` output (column names shortened):

```text
                                                       before          after           change
                                                       sec/op          sec/op
ALPChunk/decimal/ALP/write-18                         16.953µ ±  1%    2.590µ ± 3%    -84.72%
ALPChunk/exceptions/ALP/write-18                      16.724µ ±  1%    2.614µ ± 3%    -84.37%
ALPChunk/computed/ALP/write-18                        20.571µ ±  3%    8.763µ ± 0%    -57.40%
ALPChunk/random/ALP/write-18                           31.69µ ±  1%    11.48µ ± 5%    -63.76%
ALPEncode/120-18                                     15.999µ ±  5%    1.466µ ± 0%    -90.84%
ALPEncode/1024-18                                    28.035µ ±  1%    4.097µ ± 1%    -85.39%
ALPHistograms/ALPHistogram/write-18                    41.68µ ±  1%    31.70µ ± 3%    -23.96%
ALPHistograms/ALPFloatHistogram/write-18              100.00µ ±  0%    50.86µ ± 0%    -49.14%
ALPHistograms/ALPHistogram/read-18                     5.705µ ±  0%    3.917µ ± 1%    -31.35%
ALPHistograms/ALPFloatHistogram/read-18                4.665µ ±  0%    3.692µ ± 0%    -20.86%
ALPHistogramPaths/ALPHistogram/recode-18               51.61µ ± 22%    17.47µ ± 1%    -66.16%
ALPHistogramPaths/ALPFloatHistogram/recode-18         112.29µ ±  4%    24.61µ ± 1%    -78.08%
ALPHistogramPaths/ALPHistogram/query-after-append-18 2372.40µ ±  1%    32.94µ ± 1%    -98.61%
ALPHistogramPaths/ALPFloatHistogram/query-after-append-18
                                                   5448.84µ ±  2%    45.37µ ± 1%    -99.17%
ALPColdRead/ALP-18                                    541.3n ±  6%    457.6n ± 2%    -15.47%
ALPColdRead/ALPHistogram-18                           2172.0n ±  1%    763.6n ± 5%    -64.85%
ALPColdRead/ALPFloatHistogram-18                       3.006µ ± 14%    2.325µ ± 1%    -22.64%
ALPChunk/decimal/ALP/read-18                           424.4n ±  0%    443.5n ± 0%     +4.50%
ALPChunk/exceptions/ALP/read-18                        429.9n ±  0%    450.3n ± 2%     +4.75%
```

All listed differences have `p=0.002, n=6`. See the
[complete benchstat output](alp-optimization-benchstat.txt) for every case,
including unchanged codecs, allocations, and sizes.

Decimal chunk writes are 6.55 times faster than the baseline, but still cost
1.47 times XOR2's 1.763 µs. Warm decimal/exception reads increased by 19–20 ns per
120-sample chunk (about 0.16–0.17 ns/sample). This regression is retained as a
tradeoff for the cold-iterator and buffer reductions; decimal scanning remains
about twice as fast as XOR2's 875.4 ns. This measurement does not isolate the
individual cause. Unchanged XOR/XOR2 cases also show small timing shifts: the
largest statistically significant increase is 2.40% for exception XOR writes.
No encoded sizes or allocation counts changed for those codecs.

| Operation | Before allocation | After allocation | Allocations before → after |
| --- | ---: | ---: | ---: |
| Fresh float iterator, first sample | 3.469 KiB | 2.469 KiB | 5 → 4 |
| Fresh integer histogram iterator, first sample | 21.844 KiB | 3.719 KiB | 12 → 11 |
| Fresh float histogram iterator, first sample | 23.06 KiB | 12.94 KiB | 11 → 11 |
| Integer histogram recode | 46.94 KiB | 12.54 KiB | 665 → 41 |
| Float histogram recode | 55.85 KiB | 16.59 KiB | 668 → 44 |

Warmed ALP scans and isolated value encoding remain allocation-free. Complete
decimal chunk construction remains 7.094 KiB and 11 allocations. Allocation
volume per operation is not a measurement of retained Head memory.

## Compression and adaptive selection

Version 1 encoded sizes are unchanged on every measured fixture:

| 120-sample fixture | Legacy bytes/sample | ALP v1 bytes/sample | ALP v2 bytes/sample |
| --- | ---: | ---: | ---: |
| Decimal float | 5.775 | 1.308 | — |
| Decimal with stale markers | 5.700 | 1.808 | — |
| Computed float | 7.117 | 6.742 | — |
| Random-bit float | 8.483 | 8.267 | — |
| Constant float | 0.2833 | 0.3333 | — |
| Integer histogram | 8.800 | 10.68 | 6.942 |
| Float histogram | 19.39 | 17.27 | — |

Integer histogram version 2 uses 128-value numeric vectors instead of 1,024,
confining large predictor residuals to fewer values. The numeric-only experiment
measured 3.283, 2.500, 2.583, 3.683, and 6.233 bytes/sample for vector sizes 64,
128, 256, 512, and 1,024 respectively. Including all metadata, version 2 saves
about 35% over version 1 and 21% over legacy on this fixture. Its warmed scan
takes 4.094 µs versus 3.917 µs for optimized version 1, a roughly 4.5% cost for
the smaller representation. Write time is 31.80 µs. Distribution-dependent size
and speed tradeoffs remain; these fixtures do not justify forcing v2 everywhere.

To select candidates only when they save at least 5% of complete encoded bytes:

```yaml
storage:
  tsdb:
    chunk_encoding:
      floats: auto
      histograms: auto
```

`auto` uses XOR2 floats and legacy histograms in Head, then evaluates finalized
chunks during compaction. Histogram ST selection still follows the existing
feature settings. Small chunks and oversized legacy histograms can bypass trials.
Existing ALP chunks are retained. Runtime reload and an absent setting preserve
the same startup-default rules as the explicit encoding selections.

Explicit `alp` continues to write version 1. Adaptive integer histogram candidates
use [version 2](format/alp_histograms.md#format-version-2), which requires this
new reader; older version-1-only ALP binaries cannot read them. Float histogram
candidates use version 1. Streamed remote read converts both versions to existing
histogram encodings. A 5% gate protects encoded size, not CPU cost or Head memory.

## Validation

- Scalar tests passed for the affected configuration, storage, codec, and TSDB
  paths. SIMD race tests passed for codec, TSDB, and remote-read ALP cases.
- Conversion tests compare all 190 decimal parameter pairs against the scalar
  implementation with IEEE-754 boundaries, arbitrary bits, and partial vectors.
  Changing-series tests exercise hint refresh and exact ST/timestamp/value output.
- Both histogram versions cover counter/gauge/custom layouts, stale values,
  numeric limits, malformed input, append resumption, snapshots, and remote read.
  Integration covers adaptive selection/rejection, reload, compaction, restart,
  out-of-order samples, deletion, and ST preservation.
- Fuzz runs completed 1,250,387 float round trips and 2,215,912 malformed histogram
  cases, including version 2 seeds, without failure.
- Nine independently serialized version 1 fixtures passed in both directions:
  new writer → baseline reader, and baseline writer → new reader. Fixtures cover
  float modes and both histogram types, including custom buckets and varying ST.
- `make lint`, SIMD-aware ARM64 and AMD64 lint, the SIMD Prometheus build, and
  AMD64 SIMD test cross-compilation passed. Generated kernels reproduce exactly.
- The broad TSDB/storage/config/PromQL/promtool suites passed. The broad
  `cmd/prometheus` suite failed `TestRemoteWrite_PerQueueMetricsAfterRelabeling`
  with a metrics-wait timeout; the same failure was reproduced on unchanged
  baseline source. The broad run preceded final v2 changes; the final targeted
  scalar and SIMD race runs include those changes.

Only NEON executed on this host. AVX2/AVX-512 runtime tests and performance
measurements require corresponding hardware; cross-compilation and lint do not
establish their runtime correctness or speed.

## Reproduction and remaining experiments

```sh
GOTOOLCHAIN=go1.27.1 GOEXPERIMENT=simd go test ./tsdb/chunkenc \
  -run '^$' -count=6 -benchmem -benchtime=200ms \
  -bench '^Benchmark(ALPChunk|ALPEncode|ALPHistograms|ALPHistogramPaths|ALPColdRead|ALPIntegerVectorSize)$' \
  > after.txt
# Run matching benchmarks on 606f11f67 for before.txt. Copy only
# alp_paths_bench_test.go into that checkout for the added path measurements.
GOTOOLCHAIN=go1.27.1 go run golang.org/x/perf/cmd/benchstat@v0.0.0-20260908200009-22c9c6c9d4da \
  before.txt after.txt
```

The implemented snapshot and transcode changes avoid the immediate need for a
separate mutable histogram builder. A complete builder rewrite, wider float
blocks, field-major histogram layouts, timestamp/RD vectorization, and an on-disk
seek directory remain gated experiments. Representative production traces,
retained-memory measurements, compaction throughput, and full PromQL workloads
are still needed before making ALP a default. Per-series hint reuse is tested for
correctness here; the standalone chunk benchmarks do not quantify its additional
benefit during multi-chunk compaction.
