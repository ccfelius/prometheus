# ALP compared with XOR and XOR2

This benchmark directly compares the current ALP implementation with both
existing float encodings, XOR and XOR2, on identical input values and timestamps.
All start timestamps are zero because XOR cannot represent them. The earlier
[extensive benchmark](../alp-extensive-20260929/report.md) compares XOR2 and ALP
with nonzero start timestamps and also covers histogram codecs.

Measured 2026-09-30 on Apple M5 Pro, darwin/arm64,
Go 1.27.1. Source revision: `6f65296eda1740208253a777b2cf46ee5f36de65`.
There are 2,376 observations across 32 input scenarios,
two builds, three codecs, and six repetitions of each operation. The scalar and
SIMD builds use the same compiler; only `GOEXPERIMENT=simd` differs.

## Main findings

- Against **XOR**, SIMD ALP warm decoding is faster in **32/32** cases (1.00–3.62× speedup). Encoding consumes more CPU in **28/32** cases. ALP stores fewer bytes in **26/32** cases.
- Against **XOR2**, SIMD ALP warm decoding is faster in **32/32** cases (1.24–7.77× speedup). Encoding consumes more CPU in **26/32** cases. ALP stores fewer bytes in **26/32** cases.

Median process CPU utilization across rows is
99.8% of one core. Compare CPU
time for a fixed amount of data to assess CPU savings. Utilization alone does
not distinguish a fast codec from a slow one when both continuously run.

These are synthetic, memory-resident full-codec measurements. They do not
establish production ingestion or query performance, and no AMD64 machine was
tested. Case counts do not represent the frequency of patterns in production.
The counts compare observed medians and do not imply every difference is
meaningful. For example, at 1024 constant values ALP takes about 2.290 µs to
decode versus XOR's 2.298 µs, which is effectively tied.

For two-decimal values at 120 samples, ALP decodes 2.66× faster than XOR and
2.28× faster than XOR2, using about 77% fewer bytes than either. Encoding CPU
increases by about 28% versus XOR and 54% versus XOR2. Arbitrary float bits
are less attractive for storage: ALP saves only about 3% in encoded size while
using over five times the encoding CPU of either baseline.

## Compression and decompression speed

SIMD build, 120 samples per chunk, regular timestamps. Times are microseconds
for the complete chunk; lower is better. Encoding includes appending and final
serialization. Warm decoding reuses an iterator and materializes every value.

| Pattern | Codec | Encode µs/chunk | Decode µs/chunk | Encode M samples/s | Decode M samples/s |
| --- | --- | --- | --- | --- | --- |
| constant | XOR | 0.686 | 0.325 | 174.8 | 369.7 |
| constant | XOR2 | 0.558 | 0.406 | 214.9 | 295.2 |
| constant | ALP | 1.330 | 0.271 | 90.2 | 442.6 |
| counter | XOR | 1.454 | 0.641 | 82.5 | 187.2 |
| counter | XOR2 | 1.237 | 0.704 | 97.0 | 170.5 |
| counter | ALP | 2.780 | 0.373 | 43.2 | 322.0 |
| decimal2 | XOR | 2.189 | 1.026 | 54.8 | 117.0 |
| decimal2 | XOR2 | 1.827 | 0.882 | 65.7 | 136.0 |
| decimal2 | ALP | 2.836 | 0.386 | 42.3 | 310.7 |
| decimal6 | XOR | 2.611 | 1.093 | 46.0 | 109.7 |
| decimal6 | XOR2 | 1.828 | 0.911 | 65.6 | 131.7 |
| decimal6 | ALP | 7.017 | 0.382 | 17.1 | 314.2 |
| noisy-decimal | XOR | 2.446 | 1.033 | 49.0 | 116.2 |
| noisy-decimal | XOR2 | 1.951 | 0.912 | 61.5 | 131.5 |
| noisy-decimal | ALP | 2.844 | 0.394 | 42.2 | 304.6 |
| computed | XOR | 2.463 | 1.028 | 48.7 | 116.7 |
| computed | XOR2 | 1.962 | 0.909 | 61.1 | 132.0 |
| computed | ALP | 9.362 | 0.558 | 12.8 | 214.9 |
| random-finite | XOR | 2.239 | 1.100 | 53.6 | 109.1 |
| random-finite | XOR2 | 2.224 | 0.926 | 54.0 | 129.6 |
| random-finite | ALP | 10.867 | 0.559 | 11.0 | 214.8 |
| random-bits | XOR | 2.285 | 1.105 | 52.5 | 108.6 |
| random-bits | XOR2 | 2.249 | 0.943 | 53.4 | 127.2 |
| random-bits | ALP | 12.213 | 0.307 | 9.8 | 391.4 |
| stale5 | XOR | 2.674 | 1.088 | 44.9 | 110.3 |
| stale5 | XOR2 | 1.822 | 0.875 | 65.9 | 137.1 |
| stale5 | ALP | 2.842 | 0.389 | 42.2 | 308.7 |
| outliers20 | XOR | 2.045 | 1.105 | 58.7 | 108.5 |
| outliers20 | XOR2 | 1.950 | 0.951 | 61.6 | 126.2 |
| outliers20 | ALP | 7.197 | 0.395 | 16.7 | 303.9 |

## CPU cost and compressed size

CPU time is user plus system process CPU, including runtime and GC work in the
timed loop. CPU ns/sample is numerically equal to milliseconds per million
samples. Encoded bytes include chunk headers and timestamp streams. The numeric
compression ratio uses the 8-byte float value as its uncompressed baseline, excluding the
raw timestamp size; it is `8 / encoded bytes per sample` for all three codecs.

| Pattern | Codec | Encode CPU ns/sample | Decode CPU ns/sample | Encoded B/sample | Numeric compression ratio |
| --- | --- | --- | --- | --- | --- |
| constant | XOR | 5.69 | 2.70 | 0.400 | 20.00:1 |
| constant | XOR2 | 4.62 | 3.39 | 0.283 | 28.24:1 |
| constant | ALP | 10.61 | 2.26 | 0.333 | 24.00:1 |
| counter | XOR | 12.07 | 5.34 | 1.967 | 4.07:1 |
| counter | XOR2 | 10.25 | 5.87 | 1.975 | 4.05:1 |
| counter | ALP | 22.73 | 3.11 | 1.775 | 4.51:1 |
| decimal2 | XOR | 18.08 | 8.55 | 5.767 | 1.39:1 |
| decimal2 | XOR2 | 15.09 | 7.35 | 5.775 | 1.39:1 |
| decimal2 | ALP | 23.19 | 3.22 | 1.308 | 6.12:1 |
| decimal6 | XOR | 21.64 | 9.11 | 5.733 | 1.40:1 |
| decimal6 | XOR2 | 15.11 | 7.60 | 5.742 | 1.39:1 |
| decimal6 | ALP | 58.02 | 3.18 | 1.392 | 5.75:1 |
| noisy-decimal | XOR | 20.24 | 8.61 | 7.117 | 1.12:1 |
| noisy-decimal | XOR2 | 16.14 | 7.61 | 7.125 | 1.12:1 |
| noisy-decimal | ALP | 23.17 | 3.28 | 2.175 | 3.68:1 |
| computed | XOR | 20.41 | 8.57 | 7.108 | 1.13:1 |
| computed | XOR2 | 16.22 | 7.57 | 7.117 | 1.12:1 |
| computed | ALP | 77.55 | 4.66 | 6.742 | 1.19:1 |
| random-finite | XOR | 18.44 | 9.17 | 8.467 | 0.94:1 |
| random-finite | XOR2 | 18.27 | 7.72 | 8.475 | 0.94:1 |
| random-finite | ALP | 90.15 | 4.66 | 7.475 | 1.07:1 |
| random-bits | XOR | 18.83 | 9.21 | 8.483 | 0.94:1 |
| random-bits | XOR2 | 18.51 | 7.86 | 8.492 | 0.94:1 |
| random-bits | ALP | 101.20 | 2.55 | 8.267 | 0.97:1 |
| stale5 | XOR | 22.05 | 9.07 | 7.700 | 1.04:1 |
| stale5 | XOR2 | 15.05 | 7.30 | 5.700 | 1.40:1 |
| stale5 | ALP | 23.25 | 3.24 | 1.808 | 4.42:1 |
| outliers20 | XOR | 16.88 | 9.21 | 7.442 | 1.07:1 |
| outliers20 | XOR2 | 16.12 | 7.93 | 7.450 | 1.07:1 |
| outliers20 | ALP | 59.45 | 3.29 | 4.975 | 1.61:1 |

## Cold iterator decoding

These full scans allocate a fresh iterator for each chunk. The encoded bytes
remain in memory, so this is an allocation comparison, not a cold disk or CPU
cache measurement. All cases below have 120 samples and regular timestamps.

| Pattern | Codec | Decode µs/chunk | CPU ns/sample | Allocated B/chunk | Allocations/chunk |
| --- | --- | --- | --- | --- | --- |
| decimal2 | XOR | 1.058 | 8.82 | 128 | 2 |
| decimal2 | XOR2 | 0.914 | 7.61 | 144 | 2 |
| decimal2 | ALP | 0.526 | 4.29 | 1552 | 4 |
| random-bits | XOR | 1.139 | 9.48 | 128 | 2 |
| random-bits | XOR2 | 0.977 | 8.13 | 144 | 2 |
| random-bits | ALP | 0.418 | 3.41 | 1296 | 3 |

## Methodology and reproduction

The 32 scenarios contain ten patterns at 32, 120, and 1024 samples, plus two
irregular-timestamp cases at 120 samples. The fixture seed is 20260929. Both
builds use `GOMAXPROCS=1`, `GOGC=100`, `-test.cpu=1`, `-test.count=6`, and
`-test.benchtime=150ms`. Scenarios run serially in deterministic shuffled order;
the first scalar/SIMD build alternates between scenarios. Each codec receives
identical float bits and timestamps, with start timestamps set to zero.

Correctness tests compare all decoded float bits, timestamps, and start
timestamps, including NaN payloads. They also check deterministic encoded bytes.
These tests passed in both scalar and SIMD builds, and the standard scalar
`make lint` check passed. The SIMD lint configuration
has an existing formatter disagreement over the `simd/archsimd` import in
`alp_encode_arm64.go`: gofumpt requires it alongside standard imports, while
gci requires a separate group. This benchmark does not change that source.
The shared [methodology](../alp-extensive-20260929/methodology.md) documents
fixture definitions, timing boundaries, and CPU measurement details.

All operations, including other chunk sizes, irregular timestamps, scalar
results, allocations, and per-row CPU utilization, are in [summary.csv](summary.csv).
The [raw observations](results.json), [run metadata](metadata.json), and
[process diagnostics](processes.json) preserve the inputs to this report.
Six-run benchstat comparisons are available for the [SIMD build](simd-benchstat.txt)
and [scalar build](scalar-benchstat.txt). The analysis script normalizes codec
names so benchstat compares identical cases across the three files.
The percentage and significance columns in those three-file tables use XOR
as their baseline; the absolute XOR2 and ALP measurements are also included.

Run from the repository root:

```sh
GOTOOLCHAIN=go1.27.1 GOEXPERIMENT='' go test -c ./tsdb/chunkenc -o /tmp/alp-three-scalar.test
GOTOOLCHAIN=go1.27.1 GOEXPERIMENT=simd go test -c ./tsdb/chunkenc -o /tmp/alp-three-simd.test
GOMAXPROCS=1 GOGC=100 /tmp/alp-three-simd.test -test.run='^TestALPMatrixFixtures$' -test.bench='^BenchmarkALPMatrix$/^float$/.*/.*/.*/^time=no-st' -test.benchtime=1x -test.count=1 -test.cpu=1 > /tmp/alp-three-smoke.txt
GOMAXPROCS=1 /tmp/alp-three-scalar.test -test.run='^TestALPMatrixFixtures$'
python3 tsdb/docs/benchmarks/alp-extensive-20260929/run.py --scalar /tmp/alp-three-scalar.test --simd /tmp/alp-three-simd.test --smoke /tmp/alp-three-smoke.txt --out tsdb/docs/benchmarks/alp-xor-xor2-20260930 --filter '^BenchmarkALPMatrix/float/.*time=no-st' --count 6 --benchtime 150ms
python3 tsdb/docs/benchmarks/alp-xor-xor2-20260930/analyze.py
cd tsdb/docs/benchmarks/alp-xor-xor2-20260930
GOTOOLCHAIN=go1.27.1 go run golang.org/x/perf/cmd/benchstat@v0.0.0-20260908200009-22c9c6c9d4da simd-xor.txt simd-xor2.txt simd-alp.txt > simd-benchstat.txt
GOTOOLCHAIN=go1.27.1 go run golang.org/x/perf/cmd/benchstat@v0.0.0-20260908200009-22c9c6c9d4da scalar-xor.txt scalar-xor2.txt scalar-alp.txt > scalar-benchstat.txt
```

The one-iteration smoke run enumerates the cases and is excluded from the
measurements. Regenerating statistics does not require repeating measurements.
