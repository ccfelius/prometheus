# ALP next optimizations: implementation and measurements

Implemented on `tsdb-alp-next-optimizations`, based on `2af3b185a` from
`tsdb-alp-optimization`. The main optimization implementation is `2934f740d`; subsequent commits
expand remote-read coverage and validate first-sample framing before allocation.
The six requested changes were implemented in order, followed by a measured
refinement to avoid repeatedly sampling successful decimal series.

The largest measured improvements are cheaper rejection of unpromising float
chunks, fewer temporary allocations, faster float-histogram reads, smaller
histogram metadata, and direct timestamp seeks. Full ALP encoding remains more
expensive than XOR2/legacy encoding. All fixtures here are synthetic, not
production traces. ALP remains experimental and opt-in.

## 1. Reject unsuitable float trials early

`ALPEncoder.RecodeFloatIfSmaller` returns an owned ALP chunk or nil to retain
XOR/XOR2. Tiny inputs bypass conversion. For an unknown series, it reads up to
16 values and checks a bounded selection of common decimal scales and large
safe product scales. It does not run the exhaustive candidate search for this
probe. The sampled values are retained in scratch and the same source iterator
continues if a full trial is justified.

A rejection skips the next seven eligible chunks of that series, then retries.
`ResetSeries` clears that state. A recent successful decimal plan skips the
probe; the full writer still checks every value exactly and refreshes its plan
at least every sixteenth encoded block. Even a positive hint cannot bypass the
complete-chunk 5% size gate. A test changes a successful decimal series to random
bits to verify this behavior.

This is a CPU/compression tradeoff: the probe can reject a chunk that would
compress, including RD-friendly data or decimal behavior absent from the first
16 values. A changing series may wait seven chunks before another trial.
Explicit `alp` encoding retains its full search. Histogram selection retains
its existing tiny/oversized-input checks and full byte-size comparison; the
negative-result cache is currently specific to floats.

## 2. Reuse bounded compaction workspace

The compactor owns one `ALPEncoder` across series and calls `ResetSeries` at each
series boundary. Numeric hints and negative-result state are cleared, while
bounded owned scratch survives. Float conversion writes finalized blocks from
a fixed 128-sample buffer, avoiding a mutable ALP appender and repeated copies.
Histogram transcoding reuses hints, timestamp/sum bytes, numeric bytes,
predictor arrays, gathered integer fields, and materialized source histograms.
Sums are encoded directly in 128-sample blocks.

Every published chunk owns its bytes. Workspace arrays never alias a returned
chunk, and the workspace retains no source iterator or source chunk. The
encoder is not safe for concurrent use. Each variable-sized byte/numeric
scratch allocation above 64 KiB is dropped after a conversion; retained
histogram objects have a corresponding aggregate capacity limit. The bound is
per buffer, not 64 KiB for the whole encoder. A 16,384-bucket test verifies
release without invalidating the returned chunk.

## 3. SIMD in additional encoding loops

The profile identified per-field histogram prediction and decimal candidate
analysis as useful targets. New kernels process independent fields using NEON
(two uint64 lanes), AVX2 (four), or AVX-512 (eight). Each lane computes a
modulo-2^64 first difference, second difference, and zigzag value, updating the
previous value and delta. Fields are gathered once per histogram, predicted as
a vector, and copied into the numeric block buffer in spans rather than through
a per-field prediction closure.

Decimal candidate conversion remains exact. A new SIMD reduction computes
signed min/max and the rejected-lane count from converted integers and their
acceptance masks. Rejected lanes cannot influence the frame. The vector
reduction has a scalar tail and accepts both scalar masks (0/1) and SIMD masks
(0/all bits set). It is a separate vectorized pass, not a fully fused conversion
and packing kernel. Integer packing and RD encoding are still scalar.

Ordinary Go builds use matching scalar routines. Tests cover lengths 0–259,
partial vectors, random uint64/int64 values, signed extremes, all rejected
lanes, and complete histogram round trips. ARM64 kernels ran on hardware;
AMD64 kernels were cross-compiled but were not executed on this ARM host.

## 4. Histogram version 3

Adaptive compaction now trials version 3 for both integer and float histograms:

- Reset hints occupy two bits each, four per byte. Unused high bits must be zero.
- The first integer sample uses independently length-prefixed integer blocks
  of at most 128 fields instead of raw uint64 fields. Mode 0 stores raw values
  when packing would expand them; mode 1 stores a frame and packed residuals.
- Later integer blocks still contain predicted differences in 128-field blocks.
  The first sample seeds the predictor and its initial deltas are zero.
- Float histogram numeric blocks remain at most 1,024 fields; the timestamp/sum
  stream remains an ordinary version 1 ALP float chunk.

Versions 1 and 2 remain readable, and append resumption preserves each version.
Explicit `alp` still writes version 1. Older readers that only understand
versions 1/2 cannot read newly written version 3 chunks. Remote read transcodes
version 3 into the existing histogram protocol encodings. No new encoding ID or
configuration value was introduced. See the [format specification](../../format/alp_histograms.md).

The existing counter, gauge, custom bounds, changing layout, reset, large count,
stale marker, and special-float tests now include version 3. Truncation tests,
nonzero hint-padding rejection, wide first samples, and fuzz seeds also cover it.
The reader checks all first-sample block lengths and shapes before allocating
predictor arrays, so a short malformed input cannot exploit a forged wide layout.

## 5. Materialize histogram results directly

For integer histograms, `Next` restores predictor fields without building an
intermediate signed bucket slice. `AtHistogram` converts those fields directly
into the caller's bucket arrays. `AtFloatHistogram` converts and accumulates
bucket deltas directly into float buckets, preserving `Histogram.ToFloat`'s
per-delta float conversion and summation order.

For float histograms, a sample contained in one decoded numeric vector borrows
an internal slice view. A sample crossing vector boundaries is assembled in
owned scratch before either vector is overwritten. `AtFloatHistogram` copies
into the caller's result. Returned spans and buckets are independent of the
iterator and of other results. Custom bounds retain the existing immutable
interned-value convention.

Repeated `At` calls are supported, including integer-to-float calls in either
order and after mutating a previously returned bucket/span. Tests include
fractional counts and samples wider than a numeric vector. These are changes to
the existing scalar iterator API; no borrowed-buffer API was exposed to callers.

## 6. Lazy regular timestamps and direct seeks

Regular blocks retain `(start, delta)` and calculate a timestamp on demand.
They do not allocate or fill a timestamp array. Irregular blocks and mutable
tails retain the existing arrays. This also benefits histogram sum iterators.

`Seek` skips the remainder of a decoded regular block when its last timestamp
is too small, or calculates the target index using division and remainder.
The fast path first proves the block is nondecreasing without signed wrap.
Unsigned subtraction handles ranges spanning zero. Negative deltas and wrapping
sequences retain sequential behavior; malformed sections still produce errors.
Blocks' values are still decoded, including blocks skipped by seek. A future
block index or query-engine batch API is separate work.

## Measurement setup

Apple M5 Pro, darwin/arm64, Go 1.27.1 with `GOEXPERIMENT=simd` (NEON).
Six observations per benchmark, `-benchmem`, default `GOMAXPROCS=18` and GC.
Main/seek/adaptive runs use 200 ms per observation; stage/workload runs use
100 ms. These are chunk microbenchmarks; timings are per whole chunk, generally
120 samples, not per sample. Seek fixtures contain 1,024 samples.

The main [before](before.txt) is `2af3b185a`; [after](after.txt) is `e13d01cac`.
The subsequent positive-selection refinement does not affect those benchmarks;
[adaptive-final](adaptive-final.txt) measures that refinement at `2934f740d`.
The materialization/workload baseline is `27fdf5405`, with the identical new
benchmark file copied into an isolated checkout. Final first-sample allocation
validation was additionally measured in [v3-final.txt](v3-final.txt): 4.181 us
median per integer v3 chunk read, with zero warmed allocations. That validation
is newer than the main/workload measurements. The seek baseline is
`295edd837`, with the identical new seek benchmark added to an isolated checkout.

Use the raw [main benchstat comparison](benchstat.txt),
[workload comparison](workloads-benchstat.txt),
[seek comparison](seek-benchstat.txt), and
[adaptive comparison](adaptive-benchstat.txt). Historical phase measurements
are included for audit; final selection numbers supersede the earlier sampled
policy numbers in `workloads-after.txt`.

## Results

### Warm reads and seeks

| Operation | Before | After | Observed change |
| --- | ---: | ---: | ---: |
| Decimal float chunk read | 454.3 ns | 383.2 ns | -15.7% |
| Constant float chunk read | 336.5 ns | 267.6 ns | -20.5% |
| Computed float chunk read | 631.0 ns | 552.1 ns | -12.5% |
| Random float chunk read | 372.9 ns | 301.1 ns | -19.3% |
| Float histogram v1 read, eight buckets | 3.753 us | 2.875 us | -23.4% |
| Integer histogram v2 read, eight buckets | 4.138 us | 4.128 us | Approximately unchanged |
| Seek to sample 119 | 416.9 ns | 139.7 ns | -66.5% |
| Seek to sample 1000 | 3.449 us | 1.229 us | -64.4% |

Cold float iterator setup plus first `Next` falls from 2,528 to 1,536 B/op and
four to three allocations. Integer histogram cold reads fall from 3,808 to
2,752 B/op and eleven to eight allocations; float histograms fall from 13,248
to 12,192 B/op and eleven to eight allocations.

### Histogram size, complete serialized chunks

| Eight-bucket fixture | Legacy | Previous ALP | Version 3 |
| --- | ---: | ---: | ---: |
| Integer histogram, B/sample | 8.800 | 6.942 (v2) | 5.725 |
| Float histogram, B/sample | 19.39 | 17.27 (v1) | 16.52 |

The integer result is 17.5% smaller than v2 and 34.9% smaller than legacy.
The float result is 4.3% smaller than v1 and 14.8% smaller than legacy.
These fixtures use four positive and four negative buckets and computed sums.
Their float counts are whole-number float64 values. Float chunk bytes themselves
do not change in this round; the ALP decimal fixture remains 1.308 B/sample
versus XOR2's 5.775 B/sample.

### Wider histograms and integer-to-float reads

The additional fixtures have one positive span, no negative buckets, regular
15-second timestamps, and 120 increasing samples. Integer bucket populations
increase together. Fractional populations add `(bucket_index % 7)/100`, and
count is the sum of bucket populations. The 1,031-bucket case spans multiple
numeric blocks. These are deliberately controlled patterns, not a claim about
all wide histograms.

| Fixture | Legacy B/sample | ALP v3 B/sample | ALP read-float before | After |
| --- | ---: | ---: | ---: | ---: |
| 8 integer buckets | 8.675 | 5.675 | 4.171 us | 3.986 us |
| 128 integer buckets | 23.69 | 20.27 | 33.24 us | 29.89 us |
| 1,031 integer buckets | 136.6 | 119.9 | 263.6 us | 231.4 us |
| 128 fractional float buckets | 885.7 | 230.8 | 24.21 us | 23.20 us |

This comparison includes direct materialization and lazy timestamps together.
The fractional fixture demonstrates that genuinely fractional fields round-trip
and can compress well; it is not a representative compression-ratio estimate.

### Adaptive selection and workspace reuse

| Repeated float series | Full conversion trial | Final adaptive policy |
| --- | ---: | ---: |
| Decimal | 2.376 us | 2.388 us |
| Computed | 9.386 us | 113.7 ns |
| Arbitrary bits | 12.143 us | 56.2 ns |

These are amortized repeated-series results: one rejection probe followed by
seven skipped chunks, not the cost of a cold decision. The decimal path still
encodes the complete chunk. The initial always-sample implementation added
roughly 25% overhead for successful decimal chunks; reusing the positive plan
reduces the observed overhead to about 0.5%.

Reused histogram conversion workspace reduces allocations from 41 to 17 for
integer v1 conversion, and 40 to 17 for float histogram conversion in the
cold-versus-reused workspace benchmark. Reused scratch is 3,176 B/op for integer
histograms and approximately 3,977 B/op for float histograms on this fixture.
Returned chunks and source iterators still allocate. This does not promise
allocation-free compaction.

### Regressions and limits

Several unchanged controls became 2–6% slower in the main comparison, and some
wide legacy controls became 8–9% slower. This indicates host/run drift; statistical
significance within six repetitions does not remove systematic differences
between runs. All controls are retained in the raw reports. Small percentage
changes should not be treated as precise causal estimates.

Decimal ALP full writes increased from 2.615 to 2.736 us (+4.6%), alongside
XOR2 writes increasing 1.779 to 1.845 us (+3.7%). Random full writes increased
3.5%; float-histogram v1 writes increased 2.5%, alongside legacy writes increasing
4.5%. Query-after-each-append, an unchanged mutable-snapshot path, increased
about 5.5%. No general full-encoding throughput win is claimed from these runs.
The useful encode-side wins are avoiding unsuitable trials and reusing buffers.
The isolated encoding phase showed improvement, but does not establish the
same gain for the complete append/serialize path.

The report measures elapsed time, allocations, and encoded bytes. It does not
repeat the earlier process CPU-time accounting or demonstrate an end-to-end
PromQL speedup. Forced ALP still pays its full search for difficult inputs.
Broader batch iteration, field grouping, dynamic numeric block sizes, and new
outlier codecs remain benchmark-gated follow-ups, not part of this change.

## Validation

- Scalar suites passed: `go test ./tsdb/... ./storage/... ./config/... ./promql/... ./cmd/promtool/...`.
- SIMD ALP codec/compaction/remote-read tests passed under `-race`.
- Final scalar and SIMD focused tests passed after the positive-hint refinement.
- Histogram fuzzing: 823,267 executions in approximately 30 seconds, no failures.
- Float fuzzing: 1,663,183 executions in approximately 30 seconds, no failures.
- Linux/AMD64 SIMD cross-compilation passed; runtime AVX testing remains outstanding.
- Repository `make lint` passed after formatting fixes.

The complete `cmd/prometheus` suite was not rerun here. The prior branch report
records an unrelated remote-write metrics timeout reproduced on its unchanged
baseline. No claim is made that every repository test has passed.

## Reproduction

```sh
GOTOOLCHAIN=go1.27.1 GOEXPERIMENT=simd go test ./tsdb/chunkenc -run '^$' \
  -bench '^Benchmark(ALPChunk|ALPHistograms|ALPHistogramPaths|ALPColdRead|ALPSeek)$' \
  -benchmem -count=6 -benchtime=200ms
GOTOOLCHAIN=go1.27.1 GOEXPERIMENT=simd go test ./tsdb/chunkenc -run '^$' \
  -bench '^Benchmark(ALPHistogramWorkloads|ALPAdaptiveFloat|ALPWorkspace)$' \
  -benchmem -count=6 -benchtime=100ms
GOTOOLCHAIN=go1.27.1 go run \
  golang.org/x/perf/cmd/benchstat@v0.0.0-20260908200009-22c9c6c9d4da \
  before.txt after.txt
```

Run before and after in separate checkouts with the same environment. Do not
run timing benchmarks concurrently with the test suites, fuzzing, or lint.
