# Experimental ALP implementation and measurements

This branch adds lossless ALP float chunks and explicit SIMD decoding through
Go's experimental `simd/archsimd` package. The default float encoding is unchanged.
The [wire-format specification](format/alp.md) describes the architecture-independent
format and compatibility requirements.

## Build and enable

Ordinary builds use the scalar decoder and retain the repository's Go 1.26.7
minimum. To build with explicit vector instructions:

```sh
GOTOOLCHAIN=go1.27.1 GOEXPERIMENT=simd go build ./cmd/prometheus
```

Select ALP for new chunks in the Prometheus configuration:

```yaml
storage:
  tsdb:
    chunk_encoding:
      floats: alp
      histograms: alp
```

The two settings are independent and support runtime reload, start timestamps,
and out-of-order data. Histogram details and measurements are documented in
[ALP histogram chunks](format/alp_histograms.md).
Selecting ALP also converts finalized XOR/XOR2 chunks during compaction. Existing
ALP data remains readable after switching the write setting back. Older binaries
and external block readers need an ALP decoder before reading these chunks.
Streamed remote read transcodes to XOR or XOR2, preserving start timestamps.

## Implementation

- Decimal ALP uses exact encode/decode bit comparisons, bounded candidate sampling,
  integer frame-of-reference residuals, and lossless exception patching.
- Constant, raw, and ALP-RD dictionary modes cover values that do not benefit from
  the decimal transform, including arbitrary NaN payloads and negative zero.
- Compact 16-lane vertical packing handles partial blocks without rounding a
  120-sample TSDB chunk up to 1,024 samples. Writers use blocks of at most 128;
  readers accept up to 1,024 values per block.
- Fused decimal kernels unpack and reconstruct two float64 values per NEON
  instruction, four with AVX2, or eight with AVX-512. AVX2 supplies vector
  emulations for full-range int64 conversion and multiplication. Width-specific
  kernels are generated from the reviewed generic implementations.
- CPU feature dispatch selects the AMD64 backend once. Ordinary builds and
  unsupported CPUs use the same-format scalar decoder.
- Immutable completed blocks and copied iterator tails allow subsequent appends
  while an existing iterator reads its snapshot. Iterator buffers are reused.
- Integration covers chunk registration and pooling, Head, WAL replay, snapshots,
  mmap, out-of-order materialization, compaction, tombstones, config reload,
  storage adapters, remote read, and promtool's float-chunk statistics.

There is no cgo or handwritten assembly. Compiler assembly inspection of the
NEON kernels confirmed vector operations including `VAND`, `VMUL`, and
`VSCVTF V*.D2` in the generated decoders.

## Measurements

Measured on an Apple M5 Pro, darwin/arm64, Go 1.27.1, `GOEXPERIMENT=simd`, on
2026-09-28. Each case ran six times with `-benchmem -benchtime=100ms`; summaries
use `golang.org/x/perf/cmd/benchstat` version
`v0.0.0-20260908200009-22c9c6c9d4da`. These are synthetic hot-cache microbenchmarks,
not production PromQL query latency measurements.

Selected benchstat output, with paths shortened to `SIMD`:

```text
                                         │        SIMD         │
                                         │       sec/op        │
ALPDecode/decimal/120/scalar-18                    176.0n ±  1%
ALPDecode/decimal/120/native_neon-18               146.5n ±  5%
ALPDecode/exceptions/120/scalar-18                 180.3n ±  2%
ALPDecode/exceptions/120/native_neon-18            152.6n ±  8%
ALPDecode/decimal/128/scalar-18                    184.7n ±  0%
ALPDecode/decimal/128/native_neon-18               156.1n ±  0%
ALPDecode/decimal/1024/scalar-18                   1.323µ ± 10%
ALPDecode/decimal/1024/native_neon-18              1.181µ ±  2%
ALPKernel/width7/128/scalar-18                     138.8n ±  1%
ALPKernel/width7/128/native_neon-18                83.40n ±  1%
ALPKernel/width63/1024/scalar-18                   1.454µ ±  0%
ALPKernel/width63/1024/native_neon-18               807.1n ±  0%
ALPChunk/decimal/XOR/read-18                       1.012µ ±  2%
ALPChunk/decimal/XOR2/read-18                      886.1n ±  2%
ALPChunk/decimal/ALP/read-18                       430.7n ±  1%
ALPChunk/decimal/XOR/write-18                      2.123µ ± 14%
ALPChunk/decimal/XOR2/write-18                     1.760µ ±  5%
ALPChunk/decimal/ALP/write-18                      17.16µ ±  3%
```

All warmed decode and iterator cases above report zero allocations. Complete
120-sample chunk results include timestamps, headers, iterator calls, and mode
selection. Values are `float64(100000+i)/100` for decimal, decimal plus 5% staleness markers
for exceptions, `1+sin(i)/10` for computed, and arbitrary uint64 patterns for random.

| Pattern | XOR2 read ns/chunk | ALP read ns/chunk | XOR2 write µs/chunk | ALP write µs/chunk | XOR2 encoded B/sample | ALP encoded B/sample |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Constant | 369.6 | 318.0 | 0.5502 | 1.226 | 0.2833 | 0.3333 |
| Decimal | 886.1 | 430.7 | 1.760 | 17.16 | 5.775 | 1.308 |
| Exceptions | 857.8 | 441.2 | 1.760 | 16.92 | 5.700 | 1.808 |
| Computed | 910.1 | 628.1 | 1.889 | 21.16 | 7.117 | 6.742 |
| Random | 950.7 | 355.9 | 2.242 | 32.13 | 8.483 | 8.267 |

These results show the intended read/compression benefit and a substantial write
cost. Decimal ALP is about 2.06x faster to scan than XOR2 here, while encoding is
about 9.75x slower. Constructing and serializing a fresh ALP chunk allocates 7.094
KiB in 11 allocations, versus 1.844 KiB in six allocations for the decimal XOR2
case. The mutable ALP tail also retains raw samples until finalized, so encoded
size savings must not be interpreted as equal Head-memory savings. Constant ALP
chunks are larger than XOR2. These tradeoffs are why ALP remains experimental and
opt-in.

Computed values use scalar RD reconstruction, so selecting the native decimal
kernel should not improve them: the 120-value measurement was 320.6 ns versus
332.5 ns, with the same RD implementation called in both cases. No SIMD speedup
is claimed for RD or timestamp decoding.

The existing Appender/Iterator matrix was also run six times before and after
the change with Go 1.26.7. XOR/XOR2 byte sizes and allocation counts were
unchanged. Benchstat marked several small timing increases: +1.25%, +1.88%,
+1.82%, +2.47%, and a largest increase of +4.33% for the XOR2 gap/jitter,
delta-inclusive ST iterator (1.201 µs to 1.253 µs). The shared-case time geomean
was -1.10%. The XOR/XOR2 codec implementations are unchanged; this comparison
does not establish a causal performance change in them. The largest increase
is about 0.43 ns/sample in this microbenchmark and is recorded rather than hidden.

Reproduce:

```sh
GOTOOLCHAIN=go1.27.1 GOEXPERIMENT=simd go test ./tsdb/chunkenc \
  -run '^$' -count=6 -benchmem \
  -bench 'BenchmarkALP(Chunk|Decode|Kernel|Encode)$' -benchtime=100ms > alp.txt
go run golang.org/x/perf/cmd/benchstat@v0.0.0-20260908200009-22c9c6c9d4da alp.txt

go test ./tsdb/chunkenc -run '^$' -count=6 -benchmem \
  -bench 'Benchmark(Appender|Iterator)$' -benchtime=100ms > after.txt
# Run the same command on the baseline revision to obtain before.txt.
go run golang.org/x/perf/cmd/benchstat@v0.0.0-20260908200009-22c9c6c9d4da before.txt after.txt
```

## Validation and remaining limits

Tests cover all bit widths, partial SIMD lanes, independent wire-format examples,
IEEE-754 edge cases, arithmetic overflow rejection, malformed input, sample-count
limits, iterator reuse, snapshots, concurrent append, restart, configuration
changes, compaction conversion, deletion, and remote-read conversion. Both scalar
and NEON integration tests and the SIMD codec race suite passed. Fuzzing completed
203,384 round trips and 2,350,974 malformed-input cases without failure.

The affected chunk, storage, remote-read, configuration, PromQL, Prometheus, and
promtool package suites passed. The full TSDB suite passed in the earlier run;
the final broad run encountered two existing five-second block-reload timing
failures. The complete `TestBlockReloadInterval` passed three consecutive runs in
isolation. Its reload logic is unchanged by ALP.

`make lint`, SIMD-aware ARM64 and AMD64 lint checks, and the full SIMD-enabled
Prometheus binary build passed. Generated source has no lint suppressions.

The AMD64 kernels and tests cross-compile. This machine cannot execute AVX2 or
AVX-512; the added SIMD CI workflow runs each supported backend on its runner.
An AVX-512-capable runner is still needed to establish AVX-512 runtime results.
The generated kernels reproduce byte for byte.

Further optimization work includes timestamp/RD vectorization, amortizing the
encoder's parameter search across blocks, and reducing mutable-tail allocation.
Seek currently scans forward through decoded blocks; there is no on-disk block
directory. Production-scale memory, cold-cache, first-sample latency, and PromQL
performance measurements are still required before considering ALP as a default.
