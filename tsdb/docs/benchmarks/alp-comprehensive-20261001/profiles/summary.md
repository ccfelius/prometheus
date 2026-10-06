# Sampled CPU profiles

Collected October 6 from the same frozen binary as the October 1 matrix. Some
profiles attribute most samples to runtime functions, including 85.27% to
`runtime.kevent` in XOR2 encoding. The reason was not established. Treat these
as diagnostic samples; use the report's process user/system CPU measurements
for quantitative cost comparisons, and do not infer precise codec hotspots
from these percentages.

These separate five-second SIMD profiling runs provide diagnostic samples. They run after the unprofiled matrix, and their timings are excluded from the report medians. Profiles cover the whole test process, including startup and setup. Flat percentages count samples attributed directly to a function; cumulative percentages also include its callees.

## float decimal2 XOR2 encode

| Function | Flat CPU samples | Cumulative CPU samples |
| --- | --- | --- |
| `runtime.kevent` | 85.27% | 85.27% |
| `tsdb/chunkenc.(*bstream).writeBitsFast` | 7.28% | 7.97% |
| `tsdb/chunkenc.(*xor2Appender).Append` | 1.91% | 11.96% |
| `tsdb/chunkenc.alpMatrixFixture.encode` | 1.21% | 13.86% |
| `tsdb/chunkenc.(*xor2Appender).writeVDeltaKnownNonZero` | 1.04% | 7.97% |

[Flat profile](float-decimal2-XOR2-encode-flat.txt), [cumulative profile](float-decimal2-XOR2-encode-cumulative.txt), and [raw CPU profile](float-decimal2-XOR2-encode.pprof).

## float decimal2 ALP encode

| Function | Flat CPU samples | Cumulative CPU samples |
| --- | --- | --- |
| `runtime.kevent` | 51.90% | 51.90% |
| `tsdb/chunkenc.alpEncodeValuesWithState` | 12.76% | 27.07% |
| `tsdb/chunkenc.(*alpAppender).Append` | 6.55% | 10.86% |
| `tsdb/chunkenc.alpEncodeNumber` | 4.48% | 4.48% |
| `tsdb/chunkenc.alpAnalyze` | 2.41% | 6.90% |

[Flat profile](float-decimal2-ALP-encode-flat.txt), [cumulative profile](float-decimal2-ALP-encode-cumulative.txt), and [raw CPU profile](float-decimal2-ALP-encode.pprof).

## float decimal2 ALP decode

| Function | Flat CPU samples | Cumulative CPU samples |
| --- | --- | --- |
| `tsdb/chunkenc.(*alpIterator).At` | 41.27% | 41.27% |
| `tsdb/chunkenc.alpDecodeNEONWidth7` | 23.46% | 25.57% |
| `tsdb/chunkenc.(*alpIterator).Next` | 14.64% | 51.68% |
| `tsdb/chunkenc.(*alpMatrixReader).scan` | 5.64% | 99.82% |
| `tsdb/chunkenc.alpUnpackWords` | 4.59% | 8.29% |

[Flat profile](float-decimal2-ALP-decode-flat.txt), [cumulative profile](float-decimal2-ALP-decode-cumulative.txt), and [raw CPU profile](float-decimal2-ALP-decode.pprof).

## integer histogram bursty ALPv4 encode

| Function | Flat CPU samples | Cumulative CPU samples |
| --- | --- | --- |
| `tsdb/chunkenc.alpEncodePatchedIntegers` | 13.08% | 20.81% |
| `tsdb/chunkenc.(*bstream).writeBit (inline)` | 12.89% | 13.44% |
| `runtime.madvise` | 7.18% | 7.18% |
| `tsdb/chunkenc.(*bstream).writeBits` | 6.81% | 19.52% |
| `runtime.kevent` | 5.89% | 5.89% |

[Flat profile](integer-histogram-bursty-ALPv4-encode-flat.txt), [cumulative profile](integer-histogram-bursty-ALPv4-encode-cumulative.txt), and [raw CPU profile](integer-histogram-bursty-ALPv4-encode.pprof).

## float histogram noisy fractional ALPv3 encode

| Function | Flat CPU samples | Cumulative CPU samples |
| --- | --- | --- |
| `runtime.kevent` | 26.03% | 26.03% |
| `tsdb/chunkenc.alpAnalyzeRD` | 11.31% | 18.85% |
| `tsdb/chunkenc.(*bstream).writeByte (inline)` | 10.95% | 11.85% |
| `tsdb/chunkenc.(*bstream).writeBit (inline)` | 7.90% | 7.90% |
| `slices.partialInsertionSortOrdered[go.shape.uint16]` | 5.57% | 5.57% |

[Flat profile](float-histogram-noisy-fractional-ALPv3-encode-flat.txt), [cumulative profile](float-histogram-noisy-fractional-ALPv3-encode-cumulative.txt), and [raw CPU profile](float-histogram-noisy-fractional-ALPv3-encode.pprof).

## float histogram smooth ALPv4 decode

| Function | Flat CPU samples | Cumulative CPU samples |
| --- | --- | --- |
| `tsdb/chunkenc.alpRestoreTemporalNEON` | 40.03% | 47.20% |
| `tsdb/chunkenc.alpDecodeIntegersNEON` | 26.22% | 26.57% |
| `tsdb/chunkenc.alpDecodeTemporalFloats` | 7.34% | 87.41% |
| `runtime.memmove` | 5.77% | 5.77% |
| `tsdb/chunkenc.alpUnpackWords` | 4.72% | 5.94% |

[Flat profile](float-histogram-smooth-ALPv4-decode-flat.txt), [cumulative profile](float-histogram-smooth-ALPv4-decode-cumulative.txt), and [raw CPU profile](float-histogram-smooth-ALPv4-decode.pprof).
