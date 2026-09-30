# Extensive ALP benchmark methodology

## What is being compared

The tested codec implementation is the `tsdb-alp-next-optimizations` branch.
The benchmark harness is committed at `660ba97d7`. Exact binary hashes,
source hash, Go build settings, execution order, host, and revision are in
[metadata.json](metadata.json). There are two builds of the same source with
Go 1.27.1: ordinary scalar Go, and `GOEXPERIMENT=simd` using ARM64 NEON. This
avoids attributing a Go-version change to SIMD. No AMD64 execution is measured.

Float chunks compare XOR2 with ALP. Histograms compare their existing ST codecs
with ALP histogram version 3. A small reference subset also measures ALP
histogram version 1, which explicit `histograms: alp` currently writes. Version
3 is the candidate used by adaptive histogram compaction. Both builds read and
write the same formats; a scalar build can decode SIMD-written data.

## Matrix

There are 72 full-compression/decompression scenarios and seven incremental
compaction scenarios. Together they expose 351 operations per build, each with
six observations, giving 4,212 measurements.

Floats:

- Chunk sizes of 32, 120, and 1,024 samples.
- Constants; integer counters; decimals at two and six fractional digits;
  noisy two-digit decimals; sine-derived computed values; random finite values;
  arbitrary binary64 bit patterns; approximately 5% stale markers; and
  approximately 20% computed outliers mixed with decimals.
- Regular 15-second timestamps and a constant nonzero start timestamp (ST).
- Additional 120-sample decimal/computed cases with ±1-second timestamp jitter,
  or changing ST every 17 samples.

Integer and float histograms:

- 8 and 128 total positive/negative buckets, 120 samples, fixed schema 1.
- Smooth counters, bursty counters, independent gauge populations, resets every
  30 samples, a layout expansion halfway through the batch, and periodic stale
  markers. Existing appenders perform real reset cuts and layout recoding.
- Float-only fractional counters and noisy fractional counters. These contain
  genuinely fractional values, not just integer populations stored as float64.
- Smooth/bursty 8-bucket cases with 32 and 1,024 samples.
- A smooth 1,031-bucket case to cross numeric-vector boundaries.
- Integer-to-float materialization, in addition to native integer output.

Selective cold-iterator cases include all samples in the input batch; they are
not merely first-`Next` measurements. They cover 120-sample decimals, arbitrary
float bits, and 8-bucket smooth histograms.

All fixtures are deterministic synthetic data, using random seed 20260929.
They are not production traces. No universal compression ratio is implied.
Custom-bound histograms and every possible schema are not part of this timing
matrix. Correctness coverage for those remains in the codec test suite.

## Compression boundary

`encode` starts from preconstructed logical values/histograms, creates fresh
chunks and appenders, appends the complete input, and materializes all encoded
bytes. ALP histogram compression therefore includes both the existing mutable
histogram encoding and ALP serialization. It does not hide the first stage.
Fixture generation and random-number generation are excluded.

The histogram appender may replace slice headers while reconciling layouts.
The harness copies the input struct before passing it to an appender so repeated
benchmark construction cannot change the fixture. It does not add a full bucket
copy per input. A test builds each codec twice and checks byte-identical output.

Counter resets can yield several chunks per operation. Layout recoding replaces
the last chunk. The harness preserves every completed chunk and sums all encoded
bytes; it never reports only the final chunk after a reset. `samples/op` is the
number of original observations in the batch and `chunks/op` reports the actual
result. The 1,024-sample float cases deliberately force one large chunk to test
codec scaling; they do not model the normal TSDB byte-based chunk cutting policy.

The source codec remains unchanged during this work. Only benchmark and report
files were added.

## Decompression boundary

Inputs are reconstructed with `FromData` from serialized bytes before timing.
This ensures ALP histogram decoding uses the persisted numeric streams rather
than the mutable legacy snapshot shortcut.

`decode` iterates the entire input and calls `At`, `AtHistogram`, or
`AtFloatHistogram` for every sample. Histograms are materialized into reused
caller output. `decode-float` specifically measures conversion of integer
histograms to float histogram results, a path relevant to queries.

Warm decoding reuses iterator and output buffers after an untimed warmup.
`decode-cold` creates those buffers anew for each batch. Its encoded bytes are
still resident in memory. Neither mode measures cold disk/page-cache reads,
WAL I/O, index lookup, or end-to-end PromQL evaluation.

All fixtures pass `TestALPMatrixFixtures` in both builds before timing. That
checks sample counts, timestamps, ST, bitwise float values (including NaNs),
histogram validity, and equality of every decoded histogram field between
legacy and ALP output. Repeated construction is checked for fixture mutation.

## CPU accounting

The benchmark reads `getrusage(RUSAGE_SELF)` immediately before entering the
`B.Loop` loop and immediately after it finishes. This measures process user and
system CPU time, including Go runtime and garbage collection workers. It
excludes fixture construction, untimed serialization setup, and the initial
reader warmup. The Go benchmark loop's initial timer/memory-counter reset and
final loop bookkeeping fall within this CPU bracket; their small fixed cost is
amortized over each 150 ms observation. No per-sample timing syscall is added.

Reported metrics:

- `user-cpu-ns/sample`: user CPU nanoseconds per input observation.
- `sys-cpu-ns/sample`: system CPU nanoseconds per observation.
- `cpu-ns/sample`: their sum.
- `cpu-%core`: CPU time divided by timed elapsed duration, multiplied by 100.
  Thus 100% means one fully occupied CPU core, not all 18 logical CPUs.
- CPU seconds per million observations = `cpu-ns/sample / 1000`.

A histogram observation is one whole histogram, not one bucket. This matters
when comparing narrow and wide histograms. Both codecs can saturate one core
while having very different total CPU costs for the same number of samples.
CPU cost and throughput are therefore presented alongside utilization.

The runner additionally records process user/system totals, including setup,
in [processes.json](processes.json). Those diagnostic totals are **not** divided
by the timed iteration counts to produce the reported per-sample CPU costs.
The main results use the bracketed per-operation measurements above.

## Rates, size, and allocation metrics

`ns/op` measures one complete input batch. Throughput in million observations
per second is `samples/op * 1000 / ns/op`. The CSV also retains Go's numeric
payload MB/s: eight bytes per float value, or eight bytes per histogram count,
zero count, sum, and bucket field. Timestamp, ST, schema, spans, and hints are
excluded from that logical payload throughput denominator.

Encoded bytes **include** timestamps, ST, all format headers, histogram layout,
reset hints, and every resulting chunk. `encoded-B/sample` divides complete
encoded bytes by original input observations. The ratio reported against legacy
is `ALP bytes / legacy bytes`; values below one mean ALP is smaller. Numeric
payload MB/s should not be interpreted as a serialized input-byte compression
ratio.

`B/op` and `allocs/op` are Go allocation measurements per complete batch.
They measure heap traffic, not peak resident memory. Rounded zero allocations
in a long-lived iterator benchmark do not imply zero cold-start allocations.

## Execution controls and statistics

The host is an Apple M5 Pro, darwin/arm64. Both builds use `GOMAXPROCS=1`,
`GOGC=100`, and `-test.cpu=1`. Only one benchmark subprocess runs at a time.
Each scenario uses `-test.count=6 -test.benchmem -test.benchtime=150ms`.
The deterministic scenario order is shuffled with seed 20260929, and the first
build alternates scalar/SIMD between adjacent scenarios. The same scenario's
builds therefore run close together in time. Unchanged XOR2/legacy-codec rows
serve as controls for compiler/build and host variation.

Tables use medians; min/max elapsed values are retained in CSV. Raw repetitions
are included. `benchstat` provides statistical comparisons, with the usual
caution that multiple comparisons and host variation limit the interpretation
of small differences. No aggregate average over unrelated synthetic patterns
is presented as a production-workload result. CPU frequency/thermal conditions
are not pinned.

## Incremental compaction experiments

`BenchmarkALPMatrixTranscode` starts with an already-encoded legacy source and
reuses one `ALPEncoder`. It measures extra compaction work, separately from full
compression. `forced` always converts. `adaptive` invokes the actual float
selection method, or the histogram v3 trial followed by the complete 5% size
gate. It reports the retained bytes and acceptance percentage.

Repeated-source measurements represent a homogeneous series with warm hints.
For rejected float data, seven of eight eligible chunks may skip a trial. This
is an amortized policy cost, not a cold-decision latency. Histogram selection
still pays for a full candidate before rejection. These synthetic cases are
not a measurement of the entire TSDB compaction pipeline.

## Reproduce

From the repository root:

```sh
GOTOOLCHAIN=go1.27.1 GOEXPERIMENT='' go test -c ./tsdb/chunkenc -o /tmp/alp-extensive-scalar.test
GOTOOLCHAIN=go1.27.1 GOEXPERIMENT=simd go test -c ./tsdb/chunkenc -o /tmp/alp-extensive-simd.test
GOMAXPROCS=1 GOGC=100 /tmp/alp-extensive-simd.test \
  -test.run='^TestALPMatrixFixtures$' -test.bench='^BenchmarkALPMatrix' \
  -test.benchtime=1x -test.count=1 -test.cpu=1 > /tmp/alp-extensive-smoke.txt
GOMAXPROCS=1 /tmp/alp-extensive-scalar.test -test.run='^TestALPMatrixFixtures$'
python3 tsdb/docs/benchmarks/alp-extensive-20260929/run.py \
  --scalar /tmp/alp-extensive-scalar.test --simd /tmp/alp-extensive-simd.test \
  --smoke /tmp/alp-extensive-smoke.txt --out /tmp/alp-extensive-results \
  --count 6 --benchtime 150ms
python3 tsdb/docs/benchmarks/alp-extensive-20260929/analyze.py /tmp/alp-extensive-results
```

The one-iteration smoke output is used only to enumerate the benchmark plan and
validate execution; its timing/CPU results are not included in the measurements.
The runner's `--filter` option accepts a regular expression for a selected scenario.

Generate statistical comparisons from the normalized files produced by
`analyze.py`. The saved reports use this pinned benchstat version:

```sh
cd /tmp/alp-extensive-results
GOTOOLCHAIN=go1.27.1 go run golang.org/x/perf/cmd/benchstat@v0.0.0-20260908200009-22c9c6c9d4da scalar.txt simd.txt > scalar-vs-simd-benchstat.txt
GOTOOLCHAIN=go1.27.1 go run golang.org/x/perf/cmd/benchstat@v0.0.0-20260908200009-22c9c6c9d4da simd-legacy.txt simd-alp.txt > simd-codecs-benchstat.txt
GOTOOLCHAIN=go1.27.1 go run golang.org/x/perf/cmd/benchstat@v0.0.0-20260908200009-22c9c6c9d4da scalar-legacy.txt scalar-alp.txt > scalar-codecs-benchstat.txt
```
