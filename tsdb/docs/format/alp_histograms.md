# ALP histogram chunks

Select `storage.tsdb.chunk_encoding.histograms: alp` to encode both integer and
floating-count native histograms. This is independent of `chunk_encoding.floats`.
`histograms: default` selects the existing encodings. ALP histogram chunks use
encoding identifiers **8** (integer counts) and **9** (floating counts), and
always support start timestamps. They remain experimental and opt-in.

The mutable representation uses the existing ST histogram appender to maintain
schema changes, bucket expansion, counter resets, gauge semantics, and staleness.
Serialization converts numeric fields to independent ALP/packed integer streams.
The serialized format contains no embedded legacy histogram stream. Serialized
chunks decode directly; SIMD reconstructs groups of float counts and buckets as
well as sums. Integer counts never pass through a float64 conversion.

This first implementation therefore pays for both the mutable histogram encoding
and ALP serialization. `Bytes()` caches serialization until append, and chunk
size checks use the mutable representation instead of re-encoding ALP per append.
`Compact()` drops mutable state; resuming append restores it from serialized data.
Readers have separate buffers, and iterators retain immutable snapshots.

## Format, version 1

| Field | Encoding |
| --- | --- |
| Sample count | Big-endian uint16, at most 16,383. |
| Version | Byte, must be 1. |
| Counter-reset header | Byte, existing reset bits in bits 7–6; other bits zero. |
| Schema | Signed Go varint, int32 range. |
| Zero threshold | Little-endian binary64 bits. |
| Positive spans | Unsigned varint count, then signed varint int32 offset and unsigned varint uint32 length per span. |
| Negative spans | Same representation. |
| Custom bounds | Unsigned varint count, followed by little-endian binary64 bounds. |
| Sample hints | One counter-reset hint byte per sample. |
| Timestamp/sum stream size | Little-endian uint32. |
| Timestamp/sum stream | An ordinary [ALP float chunk](alp.md), storing `(ST, timestamp, sum)`. |
| Numeric stream | Count, zero count, positive buckets, then negative buckets for each sample. |

The layout is stored once because the existing appender has already reconciled
bucket expansions across the chunk. Custom bounds and the zero threshold are
fixed metadata stored exactly once. Stale samples use zero placeholders for
counts and buckets; iterators expose the standard histogram containing only the
stale sum marker. Counter-reset header bits survive conversion and append resume.
Like existing histogram iterators, the first counter sample exposes an unknown
reset hint because adjacency to an earlier chunk is not guaranteed.

For floating counts, flatten the numeric fields in sample order, then divide
them into vectors of at most 1,024 values. Each vector has a little-endian uint32
payload length followed by an ALP value block (constant, decimal, RD, or raw).
The expected vector count and final vector length follow from sample and bucket
counts; there is no padding to a whole logical vector. The existing 2/4/8-lane
NEON/AVX2/AVX-512 decoders reconstruct these fields.

For integer counts, first zigzag-encode signed bucket deltas; count and zero count
remain uint64. Store the first sample's resulting fields as raw little-endian
uint64 values. For each later sample, independently predict each field using
delta-of-delta relative to the two preceding values, with the first prior delta
zero. Differences use modulo-2^64 arithmetic; zigzag the signed interpretation
of each second difference. This is reversible across the entire uint64/int64
range. Pack vectors of at most 1,024 resulting integers using:

- Mode 0: one mode byte, followed by raw uint64 values.
- Mode 1: mode byte, width byte (0–64), unsigned uint64 frame base, then the same
  compact vertical packing used by ALP floats. Restored integers must not overflow
  the unsigned frame. The writer picks mode 1 only when smaller than mode 0.

Each integer vector also has a uint32 byte-length prefix. Decoding inverts the
predictor per field and then the bucket zigzag transform. Integer reconstruction
is scalar; float counts, buckets, and decimal sums use SIMD when enabled.

Readers check header flags, counts, varint ranges, stream lengths, dictionary
references, and numeric payload lengths. Span and numeric allocation bounds are
derived from the supplied payload. All section counts must agree, and trailing
bytes are rejected. The 16,383-sample write limit follows the mutable ST codec;
Head and OOO writers cut at that limit. Compaction splits larger legacy chunks
before converting them.

## Integration and compatibility

Selection applies to Head appends, OOO materialization, and compaction output,
including conversion of existing histogram chunks. WAL records retain logical
histograms. Mmap and snapshots can contain the new encodings; disabling ALP
writing does not disable their readers. Older binaries cannot read these chunks.

Remote read converts ALP histograms to the existing integer/float histogram
encodings, with ST variants where required, and at most 120 samples per chunk.
No new encoding identifiers are sent over the existing remote-read protocol.
Queries preserve histogram schemas, positive/negative spans, custom bounds,
start timestamps, staleness, and reset semantics.

## Initial measurements

Six runs on Apple M5 Pro, Go 1.27.1 with `GOEXPERIMENT=simd`, 120-sample chunks
using the repository's `tsdbutil.GenerateTestHistograms` and float equivalent.
These have four positive and four negative buckets and computed sums
`18.4 * (i+1)`. Fixtures are prepared outside the timed loop. Read benchmarks
include materializing the histogram using a reused destination. Selected
benchstat output:

```text
                                         sec/op         encoded-B/sample
histogramST/read                         4.975µ ± 22%          8.800
ALPHistogram/read                        5.558µ ±  1%         10.68
floathistogramST/read                    8.523µ ±  0%         19.39
ALPFloatHistogram/read                   4.585µ ±  1%         17.27
histogramST/write                        13.29µ ±  1%
ALPHistogram/write                       40.51µ ± 11%
floathistogramST/write                   25.39µ ±  2%
ALPFloatHistogram/write                  97.76µ ±  1%
```

On this fixture, float histograms are **11% smaller** and read **1.86x faster**,
at **3.85x the write cost**. Integer histograms are **21% larger** and have a
slower read median; their existing delta codec is already effective. No general
compression or speed improvement is claimed for integer histograms. Integer
ALP streams retain delta prediction to avoid the much larger regression from
packing absolute counts. Both ALP readers report zero allocations per warmed
iteration; integer B/op rounds to 1 because setup is included in allocation
accounting. Custom-bound allocation is separate from these exponential fixtures.

Reproduce with:

```sh
GOTOOLCHAIN=go1.27.1 GOEXPERIMENT=simd go test ./tsdb/chunkenc \
  -run '^$' -count=6 -benchmem -bench '^BenchmarkALPHistograms$' -benchtime=100ms
```

Encoding remains scalar. The decimal parameter search evaluates 190 candidate
exponent/factor pairs on a bounded sample and validates a shortlist. Reusing
search candidates, avoiding the intermediate mutable encoding, and vectorizing
candidate evaluation are further opportunities to reduce write cost.

Six-run before/after benchmarks of the existing ST histogram codecs showed no
significant slowdown or allocation increase: integer read 4.655 to 4.633 µs,
integer write 13.70 to 13.32 µs, float read 8.467 to 8.309 µs, and float write
26.56 to 26.08 µs. These measurements used the normal Go 1.26.7 build.
