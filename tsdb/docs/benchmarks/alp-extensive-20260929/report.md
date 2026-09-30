# Extensive ALP compression, decompression, and CPU benchmark

Measured September 29, 2026, on Apple M5 Pro, darwin/arm64, Go 1.27.1.
Implementation and benchmark revision: `660ba97d7e068bd969f973c19a8f9970cc203efa`.

**ALP is substantially better at warm full-batch decoding in this matrix, but
usually costs more CPU to compress. It does not consistently make histograms
smaller.** Adaptive selection remains important.

There are **4,212 observations**: 79 scenarios, 351 operations, two builds,
and six repetitions. The builds use identical source and compiler versions;
only the SIMD experiment differs. Both run with `GOMAXPROCS=1`, `GOGC=100`.
The timed matrix took about 12 minutes 47 seconds.

- SIMD ALP warm native decoding was faster in **72/72** full-codec scenarios,
  with observed speedups from **1.19× to 7.74×**.
- Full compression used more CPU in **66/72** scenarios; six favored ALP.
- ALP stored fewer bytes in **49/72** scenarios. This is a case count, not a
  production-workload compression estimate.
- Median CPU utilization across benchmark rows was about **99.8% of one core**.
  Both codecs keep a core busy. The meaningful resource difference is CPU time
  required to process the same number of observations.

These are deterministic synthetic, memory-resident codec benchmarks. They do
not measure end-to-end ingestion, disk I/O, or PromQL query speed. Warm decoding
materializes every value/histogram; it is not a decode-kernel-only measurement.

## Explore the complete results

- [Direct ALP versus XOR and XOR2 comparison](../alp-xor-xor2-20260930/report.md)
  adds the original XOR encoding, using identical inputs without start timestamps.
- [Interactive comparison](explorer.html): filter type, size, SIMD/scalar,
  timestamp pattern, decode mode, throughput, and CPU cost.
- [All medians in CSV](summary.csv), [machine-readable medians](summary.json),
  and [all comparison tables](tables.md).
- [Detailed methodology and reproduction](methodology.md).
- Six-run benchstat: [SIMD ALP versus legacy](simd-codecs-benchstat.txt),
  [scalar ALP versus legacy](scalar-codecs-benchstat.txt), and
  [scalar versus SIMD](scalar-vs-simd-benchstat.txt).
- [Raw observations](results.json), [per-process diagnostics](processes.json),
  [binary hashes and execution metadata](metadata.json), and individual logs in
  [raw/](raw/).

## Representative full-compression and decompression results

All rows below contain 120 input observations. Times are microseconds per whole
input batch. Histograms use the indicated total bucket count. Legacy means XOR2
for floats and the existing ST histogram codec for histograms. ALP histogram
results here are version 3. A positive size change means ALP stores more bytes.

| Input | Buckets | Compress µs legacy → ALP | Decompress µs legacy → ALP | Compression CPU ALP/legacy | Decompression CPU ALP/legacy | Encoded size change |
| --- | --- | --- | --- | --- | --- | --- |
| Two-digit decimals | 0 | 1.911 → 2.887 | 0.939 → 0.394 | 1.49× | 0.42× | -76.3% |
| Arbitrary float bits | 0 | 2.305 → 12.489 | 0.974 → 0.315 | 5.45× | 0.32× | -2.2% |
| Smooth integer histograms | 8 | 15.072 → 34.201 | 5.189 → 4.131 | 2.27× | 0.80× | -34.7% |
| Bursty integer histograms | 8 | 23.646 → 50.741 | 8.690 → 4.355 | 2.15× | 0.50× | +44.8% |
| Fractional float histograms | 128 | 445.656 → 800.376 | 127.126 → 21.064 | 1.80× | 0.16× | -76.1% |
| Noisy fractional histograms | 8 | 34.995 → 102.981 | 10.963 → 4.417 | 2.95× | 0.40× | -7.1% |

For two-digit decimals, this corresponds to roughly **62.8 → 41.6 million
samples/s for compression**, and **127.8 → 304.4 million samples/s for
decompression**. ALP uses about half again as much compression CPU, but around
58% less decompression CPU and 76% fewer stored bytes on that fixture.

For 128 fractional histogram buckets, decompression is about **6.04× faster**,
with about **83.5% less CPU** and **76.1% fewer bytes**. Compression costs about
**1.80×** the CPU. The noisy eight-bucket fractional case is less attractive:
roughly **3×** compression CPU for only **7.1%** space savings.

## CPU usage in absolute units

These are **CPU seconds per million input observations**, measured with process
user/system CPU counters around the benchmark loop. One histogram observation
means the entire histogram, not a bucket. Percent utilization alone would hide
these differences because both implementations normally occupy one core.

| Input | Legacy compression CPU s/M | ALP compression CPU s/M | Legacy decompression CPU s/M | ALP decompression CPU s/M |
| --- | --- | --- | --- | --- |
| Two-digit decimals | 0.0158 | 0.0236 | 0.0078 | 0.0033 |
| Arbitrary float bits | 0.0190 | 0.1034 | 0.0081 | 0.0026 |
| Smooth integer histograms | 0.1255 | 0.2848 | 0.0432 | 0.0344 |
| Bursty integer histograms | 0.1963 | 0.4216 | 0.0724 | 0.0363 |
| Fractional float histograms | 3.6700 | 6.5960 | 1.0580 | 0.1742 |
| Noisy fractional histograms | 0.2888 | 0.8530 | 0.0914 | 0.0368 |

Per-row median utilization ranged from about **95% to 100% of one core**.
CSV/raw data retain user CPU, system CPU, total CPU, and core utilization
separately. Process startup and fixture generation are excluded from these
per-observation costs. The outer subprocess CPU totals are retained separately
and are not used to infer compression/decompression CPU costs.

## Where forced ALP loses

The complete matrix contains 23 size regressions. Important examples include:

| Input | Buckets | Legacy B/sample | ALP B/sample | Size increase | Compression CPU ALP/legacy |
| --- | --- | --- | --- | --- | --- |
| Constant floats, 32 samples | 0 | 0.812 | 1.500 | 84.6% | 1.84× |
| Constant floats, 120 samples | 0 | 0.308 | 0.400 | 29.7% | 2.10× |
| Bursty integer histograms | 8 | 14.060 | 20.360 | 44.8% | 2.15× |
| Resetting integer histograms | 8 | 9.775 | 10.900 | 11.5% | 3.31× |
| Stale-heavy integer histograms | 8 | 11.050 | 20.020 | 81.2% | 3.99× |
| Smooth float histograms | 128 | 177.200 | 250.100 | 41.1% | 1.71× |

ALP also spends about **5.45×** the compression CPU on arbitrary float bits at
120 samples, while saving only **2.2%** of the bytes. That fails the adaptive
5% size criterion. Computed sine values save only **4.7%** at 120 samples while
requiring roughly **4.69×** the compression CPU. Their faster decoding does not
make unconditional conversion an obvious choice.

All sizes include timestamps, start timestamps, headers, layouts, hints, and
all chunks created by resets. These losses are not artifacts of excluding
metadata or counting only the last chunk.

## Chunk size and wider histograms

These controlled tests force the indicated number of samples into a batch.
The 1,024-sample float cases are scaling stress tests, not the normal TSDB
byte-based chunk-cutting policy. The full matrix covers every listed float
pattern at 32, 120, and 1,024 samples.

| Decimal samples/batch | B/sample legacy → ALP | Compress M samples/s legacy → ALP | Decompress M samples/s legacy → ALP |
| --- | --- | --- | --- |
| 32 | 6.188 → 2.656 | 53.80 → 18.39 | 125.96 → 217.02 |
| 120 | 5.800 → 1.375 | 62.79 → 41.57 | 127.73 → 304.41 |
| 1024 | 8.309 → 1.269 | 50.35 → 68.87 | 53.37 → 317.32 |

Integer-to-float materialization, which matters for query consumers, is measured separately:

| Buckets | Legacy µs/batch | ALP µs/batch | Throughput speedup | ALP/legacy CPU |
| --- | --- | --- | --- | --- |
| 8 | 5.326 | 4.051 | 1.31× | 0.76× |
| 128 | 50.019 | 27.956 | 1.79× | 0.56× |
| 1031 | 400.353 | 221.959 | 1.80× | 0.55× |

All 17 integer-to-float cases improved in this run, with speedups between
**1.29× and 6.37×**. A whole histogram is still one observation in these rates.

## SIMD contribution

Comparing ALP with legacy conflates format, predictor, timestamp, and iterator
changes with SIMD. The following table isolates SIMD by comparing ALP's scalar
and NEON builds using the same Go version and fixtures. Values above one mean
the SIMD build is faster. They are observed ratios; use the benchstat data and
unchanged-codec controls when interpreting small differences.

| Input | Buckets | Compression SIMD speedup | Decompression SIMD speedup | SIMD/scalar decompression CPU |
| --- | --- | --- | --- | --- |
| Two-digit decimals | 0 | 1.052× | 1.059× | 0.945× |
| Arbitrary float bits | 0 | 1.042× | 0.991× | 1.010× |
| Smooth integer histograms | 128 | 1.000× | 1.197× | 0.836× |
| Fractional float histograms | 128 | 1.058× | 1.115× | 0.891× |
| Noisy fractional histograms | 8 | 1.199× | 0.996× | 1.004× |

NEON is useful, but it is not the sole explanation for ALP's decoding advantage.
For example, two-digit decimal ALP decoding gains about 6% from SIMD while
ALP itself decodes about 2.38× faster than XOR2. Arbitrary-bit/raw data and
exception-heavy paths do not have the same SIMD opportunity as decimal blocks.
These results do not predict AVX2 or AVX-512 performance.

## Cold allocation and histogram version tradeoffs

Cold decoding allocates fresh iterator/result buffers and reads the entire
batch; the serialized bytes remain in RAM. Three of four primary comparisons
favored ALP; one did not. These are not first-sample-latency or cold-disk tests.

| Input | µs/batch legacy → ALP | Allocated B/batch legacy → ALP | Allocations/batch legacy → ALP | ALP/legacy CPU |
| --- | --- | --- | --- | --- |
| float / decimal2 | 0.981 → 0.534 | 144 → 1552 | 2 → 4 | 0.53× |
| float / random-bits | 1.032 → 0.430 | 144 → 1296 | 2 → 3 | 0.41× |
| integer-histogram / smooth | 6.069 → 6.418 | 880 → 4272 | 15 → 16 | 1.05× |
| float-histogram / smooth | 10.059 → 6.186 | 784 → 12896 | 15 → 15 | 0.60× |

For the eight-bucket smooth reference fixture, version 3 improves size relative to version 1 but does not necessarily minimize decode time:

| Input | Codec | B/sample | Compress µs/batch | Decompress µs/batch |
| --- | --- | --- | --- | --- |
| integer-histogram | legacy | 8.917 | 15.072 | 5.189 |
| integer-histogram | ALPv1 | 11.710 | 33.828 | 3.841 |
| integer-histogram | ALPv3 | 5.825 | 34.201 | 4.131 |
| float-histogram | legacy | 20.690 | 28.132 | 9.011 |
| float-histogram | ALPv1 | 19.770 | 55.620 | 2.958 |
| float-histogram | ALPv3 | 19.020 | 55.198 | 3.049 |

Version 1 remains the explicit histogram `alp` writer; adaptive compaction
trials version 3. The versions must not be silently mixed when comparing results.

## Incremental compaction and adaptive selection

The source is already encoded in these measurements. This is extra conversion
cost, not full compression throughput. One encoder is reused for a repeated,
homogeneous series. Float rejection hints can bypass seven of eight trials.

| Input | Buckets | µs/batch forced → adaptive | CPU s/M forced → adaptive | Adaptive accepted |
| --- | --- | --- | --- | --- |
| float / decimal2 | 0 | 2.436 → 2.489 | 0.02022 → 0.02068 | 100% |
| float / computed | 0 | 9.361 → 0.117 | 0.07790 → 0.00098 | 0% |
| float / random-bits | 0 | 12.271 → 0.067 | 0.10215 → 0.00055 | 0% |
| integer-histogram / smooth | 8 | 17.973 → 18.235 | 0.14950 → 0.15160 | 100% |
| integer-histogram / bursty | 128 | 188.043 → 187.400 | 1.56300 → 1.55900 | 0% |
| float-histogram / fractional | 8 | 28.610 → 28.668 | 0.23795 → 0.23840 | 100% |
| float-histogram / noisy-fractional | 128 | 668.145 → 667.299 | 5.50900 → 5.50250 | 100% |

Adaptive float selection removes most wasted conversion CPU for the tested
computed and arbitrary-bit series. Histogram selection still pays for a full
candidate before rejecting it: the bursty 128-bucket histogram retains legacy
bytes but consumes almost the same conversion CPU as forced ALP.

Warm parameter hints can select different decimal plans than fresh full
compression. Consequently, the incremental conversion fixture can have a
different encoded size from the cold encoder even for identical logical data.
The raw results report the actual retained size and acceptance percentage.

## A rough CPU-only break-even estimate

Dividing additional compression CPU by CPU saved per complete decode gives an
illustrative number of full read passes needed to recover the extra encoding
CPU. This ignores I/O, caching, selective queries, concurrency, and memory costs.
It is not a prediction of how many production queries make ALP worthwhile.

| Input | Buckets | Approximate complete read passes |
| --- | --- | --- |
| Two-digit decimals | 0 | 1.7 |
| Arbitrary float bits | 0 | 15.4 |
| Smooth integer histograms | 8 | 18.1 |
| Bursty integer histograms | 8 | 6.2 |
| Fractional float histograms | 128 | 3.3 |
| Noisy fractional histograms | 8 | 10.3 |

## Interpretation and limits

The strongest cases here are decimal float data and read-heavy fractional
histograms. Full encoding is usually more expensive; the six cases where ALP
compresses faster do not justify generalizing that result to other patterns.
Warm decoding is consistently favorable, but cold allocation, small chunks,
and encoded size need separate evaluation.

Keep the size gate for histograms: bursty, resetting, and stale-heavy integer
fixtures demonstrate real size regressions. A cheaper histogram rejection test
would be valuable because the present size gate avoids larger output but does
not avoid the CPU cost of constructing a rejected candidate.

The numerical results are fixture-specific. The scalar/SIMD byte sizes and
chunk counts match, all 702 reported medians contain six observations, and
input/output correctness tests passed in both builds. Go lint passed for the
harness. Statistical comparisons include all raw repetitions and unchanged
legacy controls; no end-to-end query or production-workload claim is made.

To regenerate the summaries and this report from the saved measurements:

```sh
python3 tsdb/docs/benchmarks/alp-extensive-20260929/analyze.py \
  tsdb/docs/benchmarks/alp-extensive-20260929
python3 tsdb/docs/benchmarks/alp-extensive-20260929/report.py
```
