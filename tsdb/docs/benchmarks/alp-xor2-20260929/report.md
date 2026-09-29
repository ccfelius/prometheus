# Fresh ALP versus XOR2 benchmark

Run started 2026-09-29 at 09:34:45 UTC. Revision: `5b5b11813f69474b1621c00fb47101637abf298a`. The benchmark binary was rebuilt from the clean current branch.

Apple M5 Pro, darwin/arm64, Go 1.27.1, `GOEXPERIMENT=simd` (NEON), `GOMAXPROCS=1`, `GOGC=100`. Twenty cases, six repetitions each, one-second target per repetition. Each chunk contains 120 samples with regular timestamps and no start timestamps.

Arrows mean XOR2 → ALP. Speed figures are medians. Encoded size includes headers and timestamps. Encoding includes construction, appending, and serialization; decoding scans all samples with iterator reuse. These are synthetic warm-cache codec measurements.

| Pattern | Bytes/sample | ALP space saving | Encode µs/chunk | Decode ns/chunk |
| --- | ---: | ---: | ---: | ---: |
| constant | 0.2833 → 0.3333 | -17.6% | 0.562 → 1.203 | 375.1 → 331.6 |
| decimal | 5.775 → 1.308 | 77.4% | 1.827 → 2.674 | 874.5 → 450.2 |
| exceptions | 5.7 → 1.808 | 68.3% | 1.825 → 2.728 | 860.6 → 447.1 |
| computed | 7.117 → 6.742 | 5.3% | 1.888 → 9.032 | 894.8 → 627.6 |
| random | 8.483 → 8.267 | 2.5% | 2.256 → 11.847 | 956.5 → 374.4 |

A negative saving means ALP is larger.

## Compression ratio comparison

Compression ratio here means uncompressed bytes divided by encoded bytes: larger is better. The reference is 120 timestamp/value pairs, each containing an 8-byte timestamp and an 8-byte float, or **1,920 bytes per chunk**. It excludes start timestamps, which are absent in these fixtures. Encoded sizes include all codec headers and timestamps. Thus these ratios include timestamp compression, not only compression of the float values.

Full chunk byte counts below are recovered by multiplying the reported bytes/sample by 120 and rounding to the nearest byte; the benchmark metric precision is sufficient to identify that integer unambiguously.

| Pattern | XOR2 bytes/chunk | ALP bytes/chunk | XOR2 compression ratio | ALP compression ratio | ALP smaller than XOR2 by |
| --- | ---: | ---: | ---: | ---: | ---: |
| constant | 34 | 40 | 56.47:1 | 48.00:1 | -17.65% |
| decimal | 693 | 157 | 2.77:1 | 12.23:1 | 77.34% |
| exceptions | 684 | 217 | 2.81:1 | 8.85:1 | 68.27% |
| computed | 854 | 809 | 2.25:1 | 2.37:1 | 5.27% |
| random | 1018 | 992 | 1.89:1 | 1.94:1 | 2.55% |

The decimal ALP chunk is 4.41 times smaller than its XOR2 counterpart. Constants favor XOR2; computed and random values gain little space from ALP. Ratios are specific to these fixtures and chunk sizes.

## CPU measurements

| Pattern | Encode CPU seconds/million samples, XOR2 → ALP | Encode CPU ratio ALP/XOR2 | Decode CPU seconds/million samples, XOR2 → ALP | Decode CPU ratio ALP/XOR2 |
| --- | ---: | ---: | ---: | ---: |
| constant | 0.00469 → 0.00966 | 2.06× | 0.00314 → 0.00277 | 0.88× |
| decimal | 0.01512 → 0.02186 | 1.45× | 0.00730 → 0.00376 | 0.52× |
| exceptions | 0.01512 → 0.02232 | 1.48× | 0.00718 → 0.00373 | 0.52× |
| computed | 0.01563 → 0.07487 | 4.79× | 0.00747 → 0.00524 | 0.70× |
| random | 0.01860 → 0.09832 | 5.29× | 0.00797 → 0.00312 | 0.39× |

CPU time is measured independently as child-process user + system CPU time using `resource.getrusage`, divided by the sum of benchmark iterations × 120 samples. It includes runtime, GC, process setup, and benchmark setup, but excludes compilation and analysis. Each process runs six repetitions, amortizing setup overhead. These CPU figures are aggregate measurements, not confidence intervals.

Both codecs use approximately a full core under saturation; normalized CPU cost quantifies how much work is completed for that CPU time. These numbers do not estimate total Prometheus process CPU, retained Head memory, adaptive compaction cost, or production query latency.

## Benchmark inputs and tools

The repository benchmark is [`BenchmarkALPChunk`](../../../chunkenc/alp_bench_test.go), using identical inputs for both codecs:

- Constant: every value is `1`.
- Decimal: `float64(100000+i)/100`, giving 1000.00 through 1001.19.
- Exceptions: the decimal sequence with a Prometheus stale marker every twentieth sample (6 of 120 samples).
- Computed: `1+sin(i)/10`, a floating-point gauge with values that do not follow a short decimal scale.
- Random: arbitrary IEEE-754 bit patterns from a deterministic generator with seed 42, including possible special values. This is a stress fixture.

Every case uses timestamps `1750000000000 + i*15000` milliseconds and an absent start timestamp. Both codecs run in the same Go 1.27.1 test binary; ALP uses NEON SIMD. The Go benchmark harness measures time and allocations. Python `resource.getrusage` measures CPU time, and `benchstat` compares the six repeated measurements. These fixtures do not cover real production traces, jittered timestamps, cold-cache reads, histogram codecs, or end-to-end queries. Histograms cannot be encoded with XOR2; see the separate
[histogram benchmark](../alp-histograms-20260929/report.md).

Per-repetition measurements and commands are preserved. [Measurement script](run.py), [raw results and CPU measurements](results.json), [XOR2 benchmark output](XOR2.txt), [ALP benchmark output](ALP.txt), [benchstat comparison](benchstat.txt). The combined output files normalize codec names so benchstat matches corresponding cases. No source changes were made.

To reproduce from the repository root, build the benchmark binary and run the
archived measurement script. Its output directory is outside the repository.

```sh
mkdir -p /tmp/alp-xor2-20260929-093445
GOTOOLCHAIN=go1.27.1 GOEXPERIMENT=simd go test -c -o /tmp/alp-xor2-benchmark.test ./tsdb/chunkenc
python3 tsdb/docs/benchmarks/alp-xor2-20260929/run.py
GOTOOLCHAIN=go1.27.1 go run golang.org/x/perf/cmd/benchstat@v0.0.0-20260908200009-22c9c6c9d4da \
  /tmp/alp-xor2-20260929-093445/XOR2.txt /tmp/alp-xor2-20260929-093445/ALP.txt
```

For the complete code walkthrough, see the [implementation report](../../alp-implementation-details.md).
