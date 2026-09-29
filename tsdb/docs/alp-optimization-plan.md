# ALP optimization plan

This plan starts from `606f11f67` on `tsdb-alp-simd`. It covers regular floats,
integer histograms, floating-count histograms, and their TSDB integration. It is
a plan for subsequent changes, not a claim that the optimizations already exist.
See the [implementation results](alp-optimization-results.md) for completed work,
measurements, and the remaining experiments.

Prioritize encoder work, mutable histogram reads, and memory before additional
decode kernels. Preserve the version 1 format through the first stages. Keep
ALP opt-in until ingestion, Head memory, compaction, and real queries have been
measured together.

## 1. Establish a reproducible performance budget

The existing six-run M5 Pro measurements establish the starting point:

| 120-sample fixture | Existing encoding | ALP | Interpretation |
| --- | ---: | ---: | --- |
| Decimal float write | 1.760 µs | 17.16 µs | 9.75× the encoding cost. |
| Decimal float read | 886.1 ns | 430.7 ns | 2.06× the scan speed. |
| Decimal float bytes/sample | 5.775 | 1.308 | 77% smaller. |
| Computed float write | 1.889 µs | 21.16 µs | Search costs remain high when compression is weak. |
| Random-bit float write | 2.242 µs | 32.13 µs | The most expensive fallback fixture. |
| Integer histogram write | 13.29 µs | 40.51 µs | About 3.05× the encoding cost. |
| Integer histogram bytes/sample | 8.800 | 10.68 | 21% larger. |
| Float histogram write | 25.39 µs | 97.76 µs | About 3.85× the encoding cost. |
| Float histogram bytes/sample | 19.39 | 17.27 | 11% smaller. |

Float comparisons use XOR2; histogram comparisons use the corresponding ST
codec. These are synthetic, warm-cache codec results. The histogram fixtures
have eight buckets. They do not establish production query or ingestion gains.

Extend `alp_bench_test.go` and `alp_histogram_test.go` with measurements for:

- Candidate selection, exact conversion, RD search, packing, serialization,
  decoding, and histogram materialization independently.
- Cold iterator creation and first sample, warm full scans, and sparse seeks.
- Append-only, append plus finalization, and repeated append/query interleaving.
  Include the series lock in the last case: histogram `Iterator()` currently
  calls `Bytes()`, so a query after an append can perform full ALP serialization.
- Actual 120-sample chunks and value vectors of 1, 8, 32, 64, 128, 256, 512,
  and 1,024 values. Distinguish a value vector from a TSDB chunk.
- Stable and changing decimal scales, integer-valued counters, gauges,
  constants, computed floats, stale markers, and arbitrary IEEE-754 bits.
- Histograms with 0, 8, 32, 128, and 512 buckets; sparse and dense layouts;
  integral and fractional counts; resets, gauges, and schema/layout changes.
- Regular and jittered timestamps, absent/constant/changing start timestamps,
  partially filled chunks, and out-of-order ingestion.
- Available real metric traces, with their provenance and reproducible fixture
  generation recorded. Keep synthetic data if no representative trace exists.

Collect CPU, allocation, retained-heap, and mutex profiles. Record mode,
candidate-search count, exception rate, bit width, and vector length in benchmark
diagnostics. Avoid adding per-value production metrics to the hot path.

Compare the baseline and each revision on the same machine and toolchain, in
alternating order, using six repeats and `benchstat`. Increase benchmark duration
above the current 100 ms where variance masks the result. Report both cold and
warm state; training an encoder outside the timer must not hide first-chunk cost.

Initial targets, to be confirmed by profiling rather than promised:

- Reduce decimal float finalization from 17.16 µs to at most 5 µs per 120-sample
  chunk, while keeping the existing decimal fixture at or below 1.4 B/sample.
- At least halve the computed/random fallback encoding cost.
- Keep warmed decoder allocations at zero and investigate any repeatable
  full-scan regression above 5%.
- Reduce float histogram encoding cost by at least 2×. An adaptive integer
  histogram policy should retain the legacy codec when ALP is larger.
- Track Head bytes per active series separately from encoded bytes/sample.
  No change qualifies on disk savings alone if retained memory becomes worse.

Deliverable: one benchmark/profile report with exact commands, revisions, CPU,
toolchain, state ownership, and representative workload weights.

## 2. Make scalar float encoding cheaper without changing the format

Primary files: `chunkenc/alp_value.go`, `chunkenc/alp_pack.go`, and
`chunkenc/alp.go`.

The current encoder samples up to 32 values against all 190 exponent/factor
pairs for every nonconstant block, fully evaluates five candidates, then converts
the winning candidate again. At 128 values this can mean 6,080 sampled attempts,
640 shortlist attempts, and another 128 conversions before packing.

Implement these independently measurable steps:

1. Precompute the legal signed-integer bounds for each decimal factor, and load
   scales and bounds once per candidate. Inspect generated assembly to check
   whether the existing per-value factor divisions survive compilation.
2. Separate candidate preparation, candidate evaluation, and emission. Let a
   reusable encoder workspace hold converted integers, exception positions, and
   packing scratch. Preserve the winning conversion result instead of repeating
   it during emission. Compare swapping two buffers with copying one buffer.
3. Keep the constant fast path. Add a small ordered set of common candidates,
   including integral values, then retain the exhaustive search as a fallback.
   The exact integer reconstruction and original-bit comparison remain mandatory.
4. Stop evaluating a candidate once its conservative minimum possible byte cost
   cannot beat the best complete candidate. Distinguish this safe bound from
   heuristic early acceptance, which can affect compression quality.
5. Add a bounded fast-selection policy: rank candidates on a sample, fully
   validate the likely winner, and expand the search only if its actual encoded
   cost is poor. Measure both encoding time and missed compression opportunities
   against the current exhaustive implementation.

All selection costs include block headers, packed lane tails, exceptions, and
raw/constant alternatives. A cheap candidate is not automatically a good
candidate. Keep an exhaustive benchmark oracle to quantify the tradeoff.

Acceptance: unchanged decoded bits on the entire correctness corpus, unchanged
version 1 readability, and improved cold 120-sample encoding, not only long runs.

## 3. Amortize selection across meaningful groups of data

Most normal chunks end at 120 samples, while the float writer seals at 128.
Keeping a shortlist only inside one `ALPChunk` will therefore miss the common
case. Reuse needs an owner that survives a useful amount of encoding work.

The reference ALP encoder selects a shortlist at row-group scope, then chooses
among it for individual vectors. Adapt that principle to the TSDB lifecycle;
do not require Prometheus to buffer a full future row group. See the
[reference encoder](https://github.com/cwida/ALP/blob/main/include/alp/encoder.hpp).

Introduce internal `alpEncodeState` and `alpEncodeScratch` concepts with separate
lifetimes. Names are proposed, not existing APIs:

- State: a small candidate shortlist, recent winning mode, and refresh metadata.
  It contains no borrowed sample references and is unnecessary for decoding.
- Scratch: conversion and packing buffers, owned by the current writer or
  compaction worker. Do not allocate several kilobytes per active series.

Apply reuse first where ownership is simple:

1. During compaction, retain state while encoding consecutive chunks of the same
   series; reset it at a series boundary. Separate float and histogram streams.
2. Within one large float-histogram numeric stream, reuse candidates across its
   1,024-value vectors. Sums use separate state because their distribution differs.
3. For Head, evaluate transferring a small hint from the preceding appender when
   cutting a new chunk. If persistent per-series storage is needed, measure its
   memory cost at millions of series before adopting it. Do not add a global
   label-keyed cache.
4. Give OOO materialization its own state. Never mutate in-order encoder state
   through an OOO or concurrent reader path.

Start by trying the previous winner, then the shortlist, then a full refresh.
Refresh when actual exception/byte cost deteriorates, and periodically probe
alternatives so a distribution change cannot remain stuck in an inferior mode.
Benchmark refresh intervals and thresholds; do not treat them as format rules.
After restart, reconstruct hints lazily. WAL/snapshots do not need encoder state.

Acceptance: count full searches per encoded block, include abrupt distribution
changes, and demonstrate wins on both cold and stable-series workloads without
unbounded state or lock contention.

## 4. Bound the cost of RD and incompressible data

`alpAnalyzeRD` currently sorts the high bits separately for each of 16 split
positions. This is a concrete opportunity independent of decimal SIMD.

- Sort the high 16 bits once. Smaller prefixes are right shifts of that sorted
  sequence, so obtain their frequencies by scanning merged runs. Preserve
  dictionary ordering and tie-breaking in an exact first optimization.
- Reuse a successful split or a small sampled shortlist, then evaluate the real
  dictionary and exception cost on the complete block. Reusing a split must not
  force reuse of a stale dictionary.
- Add a bounded search policy for high-entropy values. Choose raw when the tested
  alternatives cannot justify their bytes and CPU cost. A sampled heuristic can
  miss a good mode, so measure the resulting size difference explicitly.
- Keep raw and constant baselines in every benchmark. Avoid large frequency
  tables that must be cleared for every 120-value block.

Acceptance: exact round trips for arbitrary bit patterns; at least a 2× target
improvement in fallback encoding time; report compression changes separately.

## 5. Add explicit SIMD encoding to the reduced amount of work

Use the currently pinned experimental `simd/archsimd` toolchain and existing
build constraints. Optimize candidate conversion/evaluation first, packing only
if it remains important in the profile. Start with NEON on the available host,
then implement and execute the AMD64 backends on suitable hardware.

For a fixed exponent/factor candidate, process 2/4/8 values together:

1. Load original float64 values and retain their original bit patterns.
2. Perform the two scaling operations and rounding operations in the same order
   as the scalar reference. Do not fuse or reassociate floating-point operations.
3. Build masks for finite values and the supported conversion range. Replace
   invalid lanes with a safe neutral value before integer conversion.
4. Convert lanes, check integer-product bounds, reconstruct values in the exact
   current order, and compare original/reconstructed bits.
5. Reduce min/max over accepted lanes, collect exceptions, and retain converted
   integers for packing. A scalar loop over sparse exception positions is fine.

Audit the pinned API and emitted instructions before using any conversion.
Full-range float64/int64 conversions are not uniformly available on AVX2. Use a
proven restricted-domain vector method plus scalar exceptional lanes if needed;
do not execute AVX-512 instructions merely because an AVX2 backend was selected.
Test negative zero, NaN payloads, stale NaNs, subnormals, rounding boundaries,
values around 2^53, and all signed-product limits across every supported backend.

Acceptance requires assembly evidence of vector operations, scalar/SIMD
differential tests, and a reduction in complete chunk encoding time. A fast
conversion microbenchmark alone is insufficient.

## 6. Remove avoidable serialization and allocation in the mutable paths

For floats, profile geometric growth of `pending`, repeated `Bytes()` snapshots,
serialized-buffer copies, and the separate value-copy pass. Benchmark bounded
preallocation or a different pending-buffer layout. Do not embed a full large
scratch array in every Head chunk to improve an allocation benchmark.

Any buffer reuse must preserve the existing contract: bytes already returned by
`Bytes()` and iterator snapshots stay immutable after append, Compact, pool
return, or reuse. Separate mutable scratch from published storage; use copy on
write where ownership requires it. Evaluate retention across chunk pooling, not
just allocations during a warmed loop.

For histograms, address two extra traversals before replacing the appender:

1. A mutable `ALPHistogramChunk.Iterator()` should read a stable snapshot of the
   existing mutable representation without invoking ALP encoding. Reuse the
   established Head snapshot machinery where its concurrency contract permits;
   otherwise copy the necessary legacy bytes under the lock and decode outside
   it. Do not assume borrowing mutable bytes is safe.
2. Refactor immutable histogram serialization to consume a finalized source
   iterator directly. `RecodeToALPHistogram` currently decodes the source,
   re-encodes it into an ST histogram, then decodes that before writing ALP.
   Eliminate that intermediate ST construction for finalized layouts.
3. Pass the reset header and layout explicitly to this builder. Sources can be
   wrappers, so avoid asserting they are concrete histogram chunk types. Return
   errors for corrupt external sources instead of invoking the trusted mutable
   serialization panic path.
4. Reuse serialization buffers per operation or worker. Benchmark bucket copying
   and `AtHistogram`/`AtFloatHistogram` separately from numeric decoding.
5. The numeric iterator currently reserves both 1,024-float and 1,024-integer
   arrays for either histogram family. Allocate only the active kind, and size
   it for the current vector where useful. Compare cold allocations and retained
   memory under many concurrent iterators; do not optimize only warmed scans.
6. Extract layout metadata and bucket lengths without converting an entire
   integer histogram to floating counts solely to construct metadata.

Acceptance: query-after-append performs no parameter search; snapshot and race
tests pass; compaction writes equivalent values/reset semantics with fewer
traversals; retained Head memory and mutex hold time improve or stay bounded.

## 7. Improve histogram representation, with an explicit format decision

First retain version 1 and optimize its existing integer decoder: batch unpacking,
base addition, zigzag inversion, and prediction across independent fields. The
delta-of-delta predictor has dependencies over time for a field. Process fields
from the same sample in parallel where possible; a vector that crosses into the
next sample needs a boundary split or a proven prefix-sum formulation.

Then prototype alternative layouts in benchmarks before choosing version 2:

- Group count, zero count, and bucket fields with similar distributions, or tile
  a small number of buckets across a sample block. Compare with the current
  sample-major flattening, which shares a frame across heterogeneous fields.
- Compare raw integer, delta, and delta-of-delta prediction; compare applying
  prediction to signed bucket deltas before versus after zigzag. Keep exact
  uint64 counts, including values above 2^53, and modulo arithmetic explicit.
- Compare two-bit hints, a default hint plus overrides, and run encoding with the
  current one-byte-per-sample hints. Preserve the distinct chunk reset header.
- Compare first-sample integer compression with its current raw 8-byte fields.
  Avoid spending more in per-field headers than a small histogram saves.
- For float histograms, use ALP independently on suitably sized field groups.
  Arithmetic float deltas are not automatically lossless; accept them only with
  exact reconstruction checks and a lossless exception representation.
- Keep layout metadata once per stable layout. Benchmark constant/zero runs,
  empty histograms, large custom bounds, and layout expansion explicitly.

If the winning layout changes field order, hint representation, or predictor
semantics, introduce a separately specified version. Preserve version 1 readers
and fixtures, reject unknown versions, and make new writing opt-in until all
intended readers support it. CPU choice must never determine the stored format.

Replacing the mutable ST appender with a dedicated ALP histogram builder is a
later, separate change. First extract reusable layout/reset validation with
parity tests. Then accumulate bounded sample tiles directly and define how
bucket expansion rewrites the pending tile or cuts a chunk. Test schema changes,
counter resets, gauges, staleness, custom buckets, and append resumption before
removing the legacy mutable path. Measure memory: raw histogram tiles can be
substantially larger than the existing compressed mutable representation.

Acceptance: publish an integer and float histogram Pareto comparison across
bucket counts. Do not claim the integer regression is fixed based on one fixture.

## 8. Introduce a deliberate adaptive storage policy

The current explicit `alp` setting converts eligible finalized chunks even when
they become larger. Keep that setting's meaning stable. Prototype a separate
opt-in `auto` policy, starting in compaction where legacy source bytes already
exist and a comparison does not require constructing both encodings from scratch.

- Estimate whether an ALP candidate is worthwhile before paying full encode cost.
  After encoding, keep the original when the candidate fails a minimum byte-saving
  threshold. Tune that threshold against compaction CPU and query benefits.
- Preserve source encoding, timestamps, sample bounds, reset headers, and block
  statistics when retaining a source chunk. Verify chunk-pool ownership on both
  retained and replaced paths.
- Keep constants and integer histograms on their existing codecs when they win.
- Avoid trial-encoding two formats on every Head append. Evaluate a separate
  policy that keeps mutable Head cheap and applies ALP at finalization/compaction.
  Expose the persistence timing clearly if this becomes a configuration option.
- Test config switching, mixed-encoding queries, OOO chunks, remote read, and
  compaction without repeated conversion churn.

Gate: compare total bytes including headers and total ingestion/compaction CPU,
not a value-payload compression ratio alone. This policy offers a practical way
to avoid paying the current integer histogram regression while further codec
work proceeds.

## 9. Improve decoding and query behavior where profiles justify it

After the write-path changes, measure complete scans again. The current decimal
value decoder gains about 20% from NEON on the 120-value fixture; optimizing only
the fused kernel cannot account for the rest of iterator cost.

- Reduce `alpUnpackWords` clearing/copying. Prototype vector loads from complete
  little-endian rows with bounded padded scratch for compact tails. Keep all
  alignment, endianness, and mmap-boundary guarantees explicit.
- Avoid materializing all start timestamps when they are absent or constant.
  Evaluate deriving regular timestamps on demand versus vector filling. Measure
  the resulting `AtT`, `AtST`, Seek, and full-scan costs before choosing.
- Vectorize RD unpack/dictionary reconstruction and integer histogram unpacking
  only after establishing their end-to-end cost. Dictionary operations differ
  across architectures; keep a complete scalar path.
- Optimize skipping within a decoded block before adding an on-disk directory.
  An optional internal batch API can help compaction/transcoding; keep the public
  scalar iterator contract. Document that batch buffers are borrowed until the
  next iterator operation.
- Experiment with larger value blocks for immutable output separately from Head
  chunk sizing. The current float sample writer uses a fixed 128-value scratch
  array, so accepting 1,024 values in the reader does not make larger writes a
  one-line change. Larger blocks trade first-sample latency and memory for setup
  amortization. Apply existing TSDB sample, time, and byte limits consistently.
- Consider a versioned block directory only if sparse-seek workloads justify its
  space overhead. Do not increase all chunks to 1,024 samples merely to match a
  codec benchmark.

Run NEON, AVX2, and AVX-512 on real supporting CPUs. Compare AVX2 and AVX-512 on
the same capable host instead of always assuming the widest backend wins.
Report skipped architecture tests separately from passes; cross-compilation is
not execution coverage.

## 10. Deliver changes as measured, reviewable increments

Suggested dependency order:

| Change | Dependency | Persisted format |
| --- | --- | --- |
| Benchmark/profile coverage | Baseline | Unchanged. |
| Scalar conversion/search and RD improvements | Profiles | Version 1. |
| Reusable selection state in compaction/histogram streams | Search API | Version 1. |
| Mutable histogram snapshot and direct immutable transcoding | Snapshot tests | Version 1. |
| Explicit SIMD encoder, one backend at a time | Scalar oracle | Version 1. |
| Float allocation and decode improvements | Profiles and ownership tests | Version 1. |
| Adaptive compaction selection | Size/CPU measurements | Existing encoding IDs. |
| Histogram layout experiments and chosen format | Histogram corpus | Version 2 only if required. |
| Dedicated mutable histogram builder | Validation extraction and parity tests | Independent decision. |
| Larger blocks, optional batch consumers, or seek directory | Query evidence | Assess separately. |

The first implementation tranche should be the benchmark coverage, scalar search
and RD changes, state reuse during compaction, and the two histogram traversal
fixes. It addresses known structural costs before expanding SIMD or format scope.

For each performance change, run six-repeat before/after benchmarks and publish
benchstat output plus any regression. Test old fixtures with new readers, new
version 1 output with a reader built from `606f11f67`, and scalar/SIMD parity.
Selected encoder parameters may change, so byte-for-byte equality is required
only when the optimization promises identical mode/parameter selection.

Keep tests for all bit widths and vector tails, lossless IEEE-754 values, full
integer ranges, malformed/truncated input, bounded allocation, independent
golden layouts, iterator reuse, immutable snapshots, and concurrent append.
Extend integration coverage for WAL/mmap/snapshot restart, config reload,
compaction, tombstones, OOO data, and remote-read transcoding. Run `make lint` and
the relevant package/race suites before each independently compiling commit.

Finally run ingestion plus concurrent queries, compaction, and restart against a
representative workload. Report CPU, throughput, p50/p95/p99 query and append
latency, first-sample latency, RSS, retained Head bytes, GC activity, and block
size. Include `rate`, range aggregations, and histogram quantiles. Separate
already compacted reads from active-Head reads and remote-read transcoding.

Existing test limitations remain visible: the remote-write relabeling timeout
reproduced on the unchanged baseline, and AVX-512 has not been executed on the
development machine. Record these independently of new regressions. Preserve
scalar builds and readers when ALP writing is disabled throughout rollout.
