# ALP float chunks

ALP is an experimental lossless float chunk encoding with identifier **7**. It
preserves all IEEE-754 binary64 bits, including signed zero, NaN payloads and
Prometheus staleness markers, together with timestamps and start timestamps.
Its format is independent of CPU architecture and whether SIMD is enabled.

This is a Prometheus adaptation of [ALP](https://doi.org/10.1145/3626717), with
compact partial vectors. It is not byte-compatible with CWI's reference format.
Writers currently use 128-sample blocks; readers support blocks up to 1,024 samples.
The normal TSDB chunk target remains 120 samples. This block size is independent
of the two, four, or eight float64 lanes in a SIMD register.

## Chunk header

| Field | Bytes | Encoding |
| --- | ---: | --- |
| Sample count | 2 | Big-endian unsigned integer, at most 65,535. |
| Version | 1 | Must be 1. |
| Blocks | Remaining | Consecutive independent blocks, without padding. |

An empty chunk consists of the three header bytes. Block counts must sum to the
chunk sample count. Trailing bytes are invalid. Unless stated otherwise below,
multibyte fields within a block are **little-endian**.

## Block envelope

| Offset | Field | Bytes |
| ---: | --- | ---: |
| 0 | Actual sample count, 1 through 1,024. | 2 |
| 2 | Flags. | 1 |
| 3 | Reserved; must be zero. | 1 |
| 4 | Timestamp section length. | 2 |
| 6 | Start-timestamp section length. | 2 |
| 8 | Value section length. | 4 |
| 12 | Timestamp, start-timestamp, and value sections, in that order. | Variable |

Flags: bit 0 means regularly spaced timestamps; bit 1 means a start-timestamp
stream is present; bit 2 means that stream is constant. Bit 2 requires bit 1.
Other bits must be zero.

The timestamp section starts with one signed 64-bit timestamp. Regular blocks
with more than one sample then store a signed 64-bit interval; single-sample
blocks have no interval. Irregular blocks instead store `count-1` signed Go
varints of delta-of-delta, with the previous delta initially zero. Arithmetic
uses two's-complement wraparound, making differences reversible over all int64
timestamps. The containing series provides timestamp ordering.

Absent start timestamps have a zero-length section and produce zero for every
sample. Constant start timestamps occupy eight bytes. Otherwise, each sample
has a signed Go varint delta from the preceding start timestamp, initially zero.

## Value modes

Each section begins with a one-byte mode. Its declared length must exactly match
the mode's payload; decoders do not infer the sample count from padding.

| Mode | Payload after the mode byte |
| --- | --- |
| 1: Raw | `count` original uint64 floating-point bit patterns. |
| 2: Constant | One original uint64 bit pattern, repeated `count` times. |
| 3: Decimal ALP | Header, packed residuals, and exceptions as described below. |
| 4: ALP-RD | Dictionary header, low bits, indices, and exceptions as described below. |

The writer chooses the smallest measured representation after candidate analysis,
including headers and exceptions. Constant mode requires identical bits, not
floating-point equality.

## Decimal ALP

The header, including the mode byte, is 14 bytes:

| Offset | Field | Bytes |
| ---: | --- | ---: |
| 0 | Mode = 3. | 1 |
| 1 | Residual bit width, 0 through 64. | 1 |
| 2 | Exponent, 0 through 18. | 1 |
| 3 | Factor, 0 through exponent. | 1 |
| 4 | Signed frame-of-reference base. | 8 |
| 12 | Exception count. | 2 |

Residuals reconstruct integers using `int64(uint64(base) + residual)`. The
reconstructed integer must be at least the signed base. Multiply that integer
by the **integer** `10^factor`, requiring a representable signed 64-bit result,
then convert to float64 and multiply by the fixed binary64 table entry for
`10^-exponent`. Converting before integer multiplication or combining the scales
can change rounding and is not permitted.

The encoder constructs integer candidates using the two floating multiplications
`x * 10^exponent * 10^-factor`, then reference-compatible magic-number rounding.
Every candidate is decoded and compared using `math.Float64bits`. Overflow,
nonfinite values, negative zero, and other failed reconstructions are exceptions.
An exception's residual is zero, so it does not widen the frame's range.

After the residual payload, each exception occupies ten bytes: unsigned 16-bit
position followed by the original 64-bit float pattern. Positions must be unique,
strictly increasing, and below the actual sample count. Exceptions are patched
after regular decoding.

## Compact vertical bit packing

Packing always uses 16 logical uint64 lanes. Logical lane `l` contains values
at positions `l + 16*r`. Each lane is an independent LSB-first bitstream at the
block's common width. Neither the lane count nor the wire order depends on the
hardware vector width.

For `n` values and width `b`, lane `l` contains `floor((n+15-l)/16)` values. Emit
`floor(floor(n/16)*b/64)` complete word rows first. A row contains one little-endian
uint64 from each of the 16 lanes. Then emit the remaining bytes from each lane,
in lane order, rounding each lane to a byte boundary. Unused high bits are zero.

Payload length is `sum(ceil(laneCount[l]*b/8))`. At 128 values this is exactly
`16*b` bytes. Partial blocks add less than 16 bytes of byte-rounding overhead.
The decoder normalizes the suffix into zero-padded word rows; unequal lane counts
can require two suffix rows. Width zero has no payload. Width 64 has full-width
residuals and no special sentinel interpretation.

## ALP-RD

| Offset | Field | Bytes |
| ---: | --- | ---: |
| 0 | Mode = 4. | 1 |
| 1 | Low-bit count, 48 through 63. | 1 |
| 2 | Dictionary count, 1 through 8. | 1 |
| 3 | Dictionary-index width, 1 through 3. | 1 |
| 4 | Exception count. | 2 |
| 6 | Dictionary high parts, each an unsigned 16-bit integer. | `2*dictionaryCount` |

The dictionary is followed by packed low bits, packed dictionary indices, then
exceptions. Both streams use the same compact vertical layout. Dictionary entries
must fit the remaining `64-lowBitCount` bits, and indices must fit the dictionary.

Reconstruction is purely bitwise:
`bits = dictionary[index] << lowBitCount | lowBits`. An exceptional slot uses a
valid placeholder index before lookup. Its four-byte exception contains a uint16
position and the original uint16 high part. Exception positions are sorted and
unique; patch only the high part. No floating arithmetic is performed.

## Appending, querying, and SIMD

Completed blocks are immutable. A chunk retains a bounded mutable tail, snapshots
it when creating an iterator, and serializes it on `Bytes()`. Serialized snapshots
are cached until the next append. `IsFloatChunkFull` uses a conservative size bound
without recompressing the tail. `Compact()` can release the tail; appending afterward
starts another independent block. Reading mmap data never modifies the mapping.

The scalar iterator decodes one block into owned buffers. `At()` and `AtST()` only
read the current slot. There is no per-block allocation after buffers are warmed
and reused. Timestamp streams and ALP-RD currently use scalar reconstruction.
Decimal ALP uses fused SIMD unpacking, frame restoration, integer multiplication,
float conversion, and scaling when built with Go 1.27 and `GOEXPERIMENT=simd`.

The SIMD backends use NEON (two values), AVX2 (four), or AVX-512 (eight), with
width-specialized kernels generated by `go generate ./tsdb/chunkenc`. AVX2 emulates
full-range int64 conversion and multiplication using vector operations. Ordinary
builds and unsupported CPUs use the same-format scalar decoder. SIMD is a build
optimization and does not change the storage format.

## Compatibility

Select ALP with `storage.tsdb.chunk_encoding.floats: alp`. Selection is experimental
and remains opt-in. Encoding changes between ALP and XOR/XOR2 cut the active chunk
on the next append. Both ST and non-ST series are supported, and histogram encoding
selection remains independent. ALP selection also converts finalized XOR/XOR2 float
chunks when writing compacted blocks. Histogram selection is independent; see
[ALP histogram chunks](alp_histograms.md).

The WAL still stores logical samples. Snapshots and mmap chunks can contain ALP,
so reading support remains enabled when ALP writes are disabled. Older binaries
and external block readers without the ALP decoder cannot read such data. Disabling
new ALP writes does not convert already persisted chunks.

Streamed remote read converts ALP into chunks of at most 120 samples using XOR when
ST is absent and XOR2 when ST is present. ST therefore still requires an XOR2-aware
client. Native ALP chunks are not sent through the current remote-read protocol.
