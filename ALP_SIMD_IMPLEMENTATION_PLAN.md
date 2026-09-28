Implement ALP as a lossless, batch-oriented float codec with explicit Go SIMD kernels, then integrate it into Prometheus behind an opt-in encoding setting. The critical path is to unpack several integers, reconstruct them, and recover several float64 values in registers before storing the results. Buffering scalar decoding alone does not satisfy the SIMD objective.

This is the original implementation plan, researched on 2026-09-25 against repository HEAD `a0e6979d8`. The experimental implementation on branch `tsdb-alp-simd` is described in the [implementation and benchmark report](tsdb/docs/alp-implementation.md), including measured tradeoffs and remaining optimization work. References to new files and APIs below are the originally proposed names. Existing files are linked relative to this document.

1. **Use Go's experimental SIMD support explicitly.**

The relevant official package is `simd/archsimd`, enabled at build time with `GOEXPERIMENT=simd`. Go 1.26 introduced architecture-specific amd64 vectors. Go 1.27 revises that API and adds arm64 NEON and wasm support; it also introduces the higher-level portable `simd` package. These are compiler-supported standard-library experiments, rather than a dependency installed with `go get`. See the [Go 1.26 release notes](https://go.dev/doc/go1.26#simd), [Go 1.27 release notes](https://go.dev/doc/go1.27), and [versioned archsimd documentation](https://pkg.go.dev/simd/archsimd@go1.27.1).

Use `simd/archsimd` for the first optimized ALP implementation because its fixed vector widths and explicit instruction requirements fit the unpacking kernels. Ordinary scalar Go remains the correctness reference and portable implementation. Handwritten assembly and cgo are not prerequisites for this approach.

The repository's [go.mod](go.mod) declares Go 1.26.7, while [.github/workflows/ci.yml](.github/workflows/ci.yml) already uses Go 1.27.x in several jobs. Initial recommendation: retain the module baseline, compile the SIMD implementation only with Go 1.27 or later and the experiment enabled, and retain the scalar implementation for ordinary builds. Pin the exact tested 1.27 patch release in the experimental CI job because the API is unstable.

Use mutually exclusive backend selection files with constraints equivalent to:

```go
// AMD64 SIMD selection.
//go:build go1.27 && goexperiment.simd && amd64

// ARM64 SIMD selection.
//go:build go1.27 && goexperiment.simd && arm64

// Scalar selection when an experimental backend is unavailable.
//go:build !go1.27 || !goexperiment.simd || (!amd64 && !arm64)
```

Keep the scalar kernel itself available in every build for differential testing and CPU fallback. Experimental imports must occur only in files whose constraints enable them. Building an ordinary binary must never require `GOEXPERIMENT=simd`, and data written by a SIMD-enabled binary must remain readable by a scalar-only binary of the same ALP-capable release.

Before implementing the codec, compile a small isolated probe with the pinned toolchain that exercises loads, shifts, masks, addition, integer multiplication, integer-to-float conversion, float multiplication, and stores. Inspect generated instructions and record the CPU feature needed by each operation. This establishes that the experimental API can implement the intended kernel on the actual development and benchmark machines.

2. **Establish the repository constraints before choosing a storage format.**

| Existing behavior | Relevant code | Consequence for ALP |
| --- | --- | --- |
| Float chunks currently use XOR or XOR2. | [chunk.go](tsdb/chunkenc/chunk.go), [xor2.go](tsdb/chunkenc/xor2.go) | Add a distinct ALP encoding; XOR2's joint timestamp/value control stream cannot directly feed independent value lanes. |
| The default target is 120 samples, with further time, sample-count, and byte limits. | [head.go](tsdb/head.go), [head_append.go](tsdb/head_append.go) | Benchmark small batches first. A 1,024-value compression vector will rarely fit the current ordinary chunk lifecycle. |
| Generic series-to-chunk conversion also splits at 120 samples. | [series.go](storage/series.go) | Changing only Head's chunk target would leave other paths inconsistent. |
| `Iterator` exposes one current sample through `Next`, `At`, `AtT`, and `AtST`. | [chunk.go](tsdb/chunkenc/chunk.go) | Decode a batch into iterator-owned storage and serve scalar calls from it. |
| Append preprocessing calls `len(chunk.Bytes())` to decide when to cut. | [head_append.go](tsdb/head_append.go), [querier.go](tsdb/querier.go) | A buffered `Bytes()` implementation must not trigger full recompression before nearly every append. |
| Head locks only while constructing an iterator; iteration then continues unlocked. | [head_read.go](tsdb/head_read.go) | Capture immutable data and snapshot any mutable tail before releasing the lock. |
| `Compact()` explicitly allows future appends. | [chunk.go](tsdb/chunkenc/chunk.go) | It is not a guaranteed finalization or sealing event. |
| Start timestamps are supported by XOR2 and used by ingestion, replay, and querying. | [xor2.go](tsdb/chunkenc/xor2.go), [head_test.go](tsdb/head_test.go) | ALP must preserve the `(start timestamp, timestamp, value)` triple. |
| Streamed remote read casts internal encoding IDs directly into protobuf enum values. | [codec.go](storage/remote/codec.go), [types.proto](prompb/types.proto) | Prevent an internal ALP encoding from silently reaching clients that cannot decode it. |

There is also an obsolete `ALPBuffered` branch in [benchmark_test.go](tsdb/chunkenc/benchmark_test.go) that permits an absolute error of `1e-6`, although only XOR and XOR2 are currently registered. Remove this exemption when adding ALP. Use bitwise float comparisons. An ALP implementation that needs approximate comparison is incorrect for this task.

3. **Separate three different meanings of “vector.”**

| Unit | Initial choice | Purpose |
| --- | --- | --- |
| Hardware vector | 2 float64 lanes on NEON, 4 on AVX2, 8 on AVX-512 | Execute the same reconstruction operation on multiple values simultaneously. |
| Compression block | Prototype up to 128 actual samples; also build a 1,024-value reference benchmark | Share the exponent, factor, frame-of-reference base, bit width, and exceptions. |
| Sampling group | Bounded set of already-available blocks from one series | Amortize parameter search without introducing cross-series decoding dependencies. |

The ALP reference uses 1,024-value vectors and larger row groups for sampling. Those are algorithm-level batching choices, not the number of doubles in one CPU register. The relevant definitions are in [ALP's configuration](https://github.com/cwida/ALP/blob/main/include/alp/config.hpp) and [sampling implementation](https://github.com/cwida/ALP/blob/main/include/alp/sampler.hpp).

Implement and measure both a reference-sized 1,024-value codec and a compact 128-value adaptation. Use the latter as the initial TSDB candidate because it works with 120-sample chunks and real short tails. Preserve the actual sample count; a 120-sample chunk must not become a persisted 1,024-slot padded vector.

Do not increase the global chunk target as part of the first implementation. Larger chunks require a separate evaluation of Head memory, sparse seeks, query latency, time boundaries, mmap behavior, compaction, and remote-read framing. The 1,024-value mode provides a useful comparison and a possible later immutable-block optimization.

The smaller-block layout below is a Prometheus adaptation of ALP and FastLanes. It does not claim binary compatibility with the CWI C++ implementation. Scalar, AVX2, AVX-512, and NEON backends must nevertheless share exactly the same Prometheus wire format.

4. **Specify lossless scalar arithmetic before optimizing it.**

Create internal value-block primitives, initially independent of timestamps and the `Chunk` interface. Suggested files are `alp_codec.go`, `alp_tables.go`, `alp_scalar.go`, and `alp_codec_test.go` within `tsdb/chunkenc`.

Pin the CWI reference revision and copy or generate its constant tables with provenance and any required license notices. The inspected implementation uses exponent and factor indices up to 18, yielding 190 pairs with `0 <= factor <= exponent <= 18`. Do not mix those constants with descriptions of older variants that search a different range. Store floating constants with reproducible binary64 values. See [the reference constants](https://github.com/cwida/ALP/blob/main/include/alp/constants.hpp).

For a selected exponent `e` and factor `f`, candidate integer construction is conceptually:

```text
scaled1 = roundToBinary64(x * pow10Positive[e])
scaled2 = roundToBinary64(scaled1 * pow10Negative[f])
q       = referenceCompatibleRoundToInteger(scaled2)
```

The rounding routine must reproduce the reference's supported-domain behavior and explicit intermediate rounding. Its implementation uses `(scaled2 + C) - C`, with `C = 6755399441055744.0` and binary64 rounding at each step. Do not substitute `math.Round`, which has a different tie rule, or assume that arbitrary large values can safely pass through the trick. Check both the scaled input's supported domain and the rounded result before converting to int64; in particular, float64 cannot represent `MaxInt64` exactly, so use a strict upper bound of `2^63`. Source: [the reference encoder](https://github.com/cwida/ALP/blob/main/include/alp/encoder.hpp).

The inspected reference's decode arithmetic has a particularly important detail: `FACT_ARR` is an integer table. Integer multiplication precedes conversion to float:

```text
scaledInteger = checkedInt64Multiply(q, pow10Integer[f])
decoded       = float64(scaledInteger) * pow10Negative[e]
```

This is not interchangeable with converting `q` first or multiplying it by a precombined floating scale. Preserve operation order in the scalar and SIMD implementations. Source: [the reference decoder](https://github.com/cwida/ALP/blob/main/include/alp/decoder.hpp).

For this implementation, make representable signed-64-bit products a mainline invariant. A candidate that would overflow goes through the exception path. This makes arithmetic behavior explicit instead of inheriting C++ signed-overflow behavior. Validate the same invariant when reading malformed blocks, using block bounds when sufficient and lane checks otherwise.

Accept a value into the integer stream only when:

```go
math.Float64bits(decoded) == math.Float64bits(original)
```

Store every other value as an exception containing its position and original `uint64` bits. Explicitly cover negative zero, infinities, subnormals, signaling and quiet NaN payloads, extreme finite magnitudes, and Prometheus's stale-NaN marker. Ordinary `==` cannot establish this contract.

For frame-of-reference encoding, compute the minimum and maximum of the accepted integers. Fill exception positions with an accepted integer such as the frame base, so they do not widen the packed range. Handle zero accepted values by selecting raw storage. Compute differences in unsigned arithmetic:

```text
base        = minimum accepted signed integer
range       = uint64(maximum) - uint64(base)
width       = bits.Len64(range)
residual[i]  = uint64(q[i]) - uint64(base)
q[i]        = int64(uint64(base) + residual[i])
```

This handles ranges crossing zero without overflowing signed subtraction. Define widths 0 through 64 explicitly: width 0 needs no residual payload; width 64 must not evaluate a shift or mask expression intended only for widths below 64.

Avoid a scalar unpacking prepass solely for arithmetic validation. For positive integer factor `P`, legal integers lie between `MinInt64/P` and `MaxInt64/P`, with Go's truncation toward zero giving the required lower bound. If the base is in that range and the maximum residual allowed by the width is at most `uint64(upperBound)-uint64(base)`, the header proves product safety. Otherwise fuse bounds checks with lane reconstruction and reject the block before exposing results. Also reject residuals that would move the reconstructed signed integer below the base through wraparound.

Use a small internal API that keeps validation, arithmetic, and architecture dispatch separate:

```go
// Proposed internal API shape, not an existing API.
func encodeALPBlock(dst []byte, values []float64, work *alpEncodeScratch) ([]byte, error)
func parseALPBlock(src []byte) (alpBlockView, error)
func decodeALPBlock(dst []float64, block alpBlockView, work *alpDecodeScratch) error
```

`alpBlockView` borrows validated byte ranges. Decoder destinations and scratch storage belong to the caller. Document lifetimes where these APIs are defined. A block decoder must report malformed input without reading beyond the supplied data or exposing partially decoded results as valid samples.

5. **Make parameter selection adaptive and account for all bytes.**

Implement the reference-style two-stage search as a baseline: identify a small shortlist of exponent/factor pairs from a bounded sample, then select among those candidates for each block. Sampling is an encoder optimization; a decoder must receive all required parameters in the block itself.

For immutable encoding, sample already-available consecutive blocks from one series. For mutable Head encoding, search the first full or finalized partial block, then reuse and reassess a bounded shortlist as more data arrives. Never wait for a large row group of future samples, and never require persistent global state to decode a chunk.

Use up to five candidates initially. Try a cheap integral candidate for integer-valued data and benchmark shortcuts for constant data. If a shortlist becomes poor, rerun bounded sampling or choose a fallback. Reusing candidate pairs must not lock a changing metric into an expensive exception-heavy representation.

Choose using the complete encoded cost, not just the residual bit width:

```text
decimal ALP = mode/header bytes
            + packed residual bytes
            + exception positions
            + original exception bits

ALP-RD      = mode/header bytes
            + dictionary bytes
            + packed dictionary indices
            + packed low bits
            + dictionary exceptions

raw         = mode/header bytes + 8 * actualCount
constant    = mode/header bytes + one original uint64 value
```

The final decision must use actual encoded bytes after evaluating a candidate, since a sample can miss outliers. For the first correct implementation, selecting raw over an unexpectedly bad ALP candidate is sufficient; searching additional candidates can be optimized later. Choose constant mode only when all original bit patterns match.

At the whole-chunk level, also compare against XOR/XOR2 during evaluation. Slowly changing and constant metric series can be very efficient under the existing codecs. A favorable decimal-values benchmark is not evidence that ALP improves every metric workload. Make any eventual mixed-codec writer policy explicit; mode decisions inside an `EncALP` chunk do not change its outer encoding ID.

6. **Use a packed layout that feeds multiple independent lanes.**

Keep timestamps and start timestamps separate from values. Use frame-of-reference residuals, without delta-coding the values against their immediate predecessor. The latter would add a dependency chain to reconstruction.

Prototype a fixed, architecture-independent arrangement of 16 logical lanes. Logical lane `l` contains samples at positions `l`, `l+16`, `l+32`, and so forth. Hardware processes those logical lanes in groups of 2, 4, or 8. In the reference 1,024-value case, each lane has 64 residuals and occupies exactly `width` 64-bit words; corresponding words from all lanes are adjacent. This arrangement is visible in [the generated fused ALP decoder](https://github.com/cwida/ALP/blob/main/src/falp.cpp).

For the small-block variant, propose this exact compact-tail rule for the prototype:

- Compute the actual residual count in each logical lane, including zero-length lanes.
- Pack each lane LSB-first with the block's common bit width.
- Emit the complete 64-bit word rows common to every lane in row-major order, with little-endian words.
- Emit the remaining bits of each lane as a byte-rounded suffix, in lane order. Set unused high bits of the last byte to zero.
- On decode, expand the compact suffix into zero-initialized scratch word rows and run the same extraction logic over those rows.

For 128 values, every lane has eight residuals, so its payload occupies exactly `width` bytes and total payload is `16 * width`. For an arbitrary short block, wire size is `sum(ceil(laneCount[l] * width / 8))`; byte-rounding adds less than 16 bytes over ideal bit packing. Because lane counts may differ by one, a suffix can require up to two scratch rows, or 256 bytes. It must not assume every suffix fits one 64-bit word per lane.

For example, 128 residuals at width 10 occupy 160 bytes: one full 128-byte row followed by a two-byte suffix for each of 16 lanes. An AVX2 operation can extract and reconstruct four residuals from a row simultaneously. Loop over four groups to cover the 16 logical lanes and repeat for each sample row. Output stores recover original sample order directly.

Benchmark logical lane counts 4, 8, and 16 before freezing the format. Sixteen aligns with the reference structure; fewer lanes may reduce setup costs for very short blocks. The persisted layout must use a specified lane count or layout identifier, never a count inferred from the reader's CPU.

Generate width-specific extraction kernels for widths 0 through 64 from one maintained generator. Give the scalar reference an independently understandable pack/unpack implementation so a generator error is not automatically duplicated by the oracle. Use golden packed-byte fixtures for representative widths and tails.

The extraction for a residual spanning two words is conceptually:

```text
residual = ((currentWord >> shift) | (nextWord << (64-shift))) & mask
```

Generate separate cases for aligned values, cross-word values, width 0, and width 64. This avoids undefined or architecture-dependent shift edge cases and makes most shifts compile-time constants. Check input bounds before entering a kernel. Never issue a vector load past an mmap boundary merely because the allocation might normally have spare capacity.

7. **Build fused decode kernels with explicit CPU requirements.**

The hot path for a decimal block should perform this sequence inside one width-specialized kernel:

```text
load packed word vectors
    -> shift/mask/merge to unpack several unsigned residuals
    -> add broadcast frame base
    -> multiply signed integers by the fixed integer factor
    -> convert several int64 values to float64
    -> multiply by the broadcast inverse exponent
    -> store several decoded values
```

Keep intermediate residuals and integers in registers. A staged implementation that first writes an entire `[]int64` block is useful for correctness and comparison, but the production candidate should fuse unpacking and reconstruction to avoid that round trip through memory. Broadcast block parameters once outside the inner loop. Choose the width-specialized function once per block.

After decoding the regular lanes, apply the sparse exception list by assigning `math.Float64frombits(exceptionBits)` to each exception position. A scalar patch loop is an appropriate starting point: it keeps unpredictable branches out of the main SIMD path. Optimize exception handling only if profiles justify it. Exception-heavy blocks should usually have selected a different representation.

| Backend | Simultaneous float64 values | Implementation detail |
| --- | --- | --- |
| Scalar Go | 1 | Full format support, correctness oracle, all supported build targets. |
| amd64 AVX2 | 4 | Vector unpack/add; explicitly emulate 64-bit integer factor multiplication and signed-64-bit conversion. |
| amd64 AVX-512 | 8 | Direct integer multiply and conversion where the required features are present. Also benchmark narrower kernels. |
| arm64 NEON | 2 | Vector unpack/add and signed-64-bit conversion; implement factor multiplication using appropriate integer sequences. |

On amd64, `Int64x4.ConvertToFloat64()` maps to `VCVTQQ2PD` and `Int64x4.Mul()` maps to `VPMULLQ`; despite their four-lane width, both require AVX-512. Unsigned shifts and bitwise unpack operations are suitable for AVX2. Use the pinned package's documented feature checks; its AVX-512 grouping covers multiple subfeatures. See [the versioned amd64 operations](https://github.com/golang/go/blob/go1.27.1/src/simd/archsimd/ops_amd64.go).

For the AVX2 conversion, port and independently test the reference's full-range `int64_to_double_fast_precise` construction using integer bit operations followed by floating subtraction and addition. Preserve its operation ordering. Its smaller magic-number conversion has a restricted range and cannot be substituted for arbitrary signed-64-bit values. See [the ALP conversion helpers](https://github.com/cwida/ALP/blob/main/include/alp/decoder.hpp).

For factor multiplication on AVX2 and NEON, exploit that the factor is a block-wide constant. Factor zero means multiplication by one and should disappear. Candidate implementations are generated shift/add chains or widening 32-bit partial products. Repeated multiplication by ten is a simple reference implementation, but may be too costly for large factors. Perform synthesis on unsigned lane bit patterns modulo `2^64`, then interpret the result as signed. Intermediate wrap is permitted; the final product must meet the checked signed mainline contract. The arm64 conversion maps to NEON `SCVTF`; verify all used operations against [the arm64 source](https://github.com/golang/go/blob/go1.27.1/src/simd/archsimd/ops_arm64.go).

Dispatch once per iterator or block family, with scalar selection available for tests and benchmarking. Never select unsupported instructions because a machine merely reports “AVX2.” Benchmark AVX-512 against AVX2 on the same host rather than assuming twice the lane count guarantees better throughput.

Inspect the compiled inner loops with `go tool objdump`. Evidence of completion includes packed loads, shifts, integer operations, conversions or their vector emulation, packed floating multiplication, and vector stores. Look for unexpected helper calls, lane extraction loops, spills, repeated bounds checks, and allocations. The scalar comparison must use the same wire format and workload.

8. **Implement ALP-RD as the second algorithmic mode.**

Raw fallback makes the decimal codec lossless for every input, but it does not implement ALP's adaptive high-precision mode. Include ALP-RD in the complete implementation, after the decimal path is correct and measured. The reference separates a floating value's original bits into a dictionary-compressed high portion and a directly packed low portion, with exceptions for high portions absent from the dictionary. See [the combined ALP-RD encoder and decoder](https://github.com/cwida/ALP/blob/main/include/alp/rd.hpp).

Implement split-position search, a bounded high-part dictionary, dictionary-index packing, low-bit packing, and exceptions. The current double-precision reference considers low-bit counts 48 through 63, up to eight `uint16` high-part dictionary entries, and one to three bits per dictionary index. A high-part exception uses a `uint16` position and `uint16` original high part. Start with these limits and compare actual RD bytes with decimal ALP, raw, and constant modes.

Decode without floating arithmetic:

```text
rawBits[i] = (uint64(dictionary[index[i]]) << lowBitCount) | lowBits[i]
value[i]   = bitCastFloat64(rawBits[i])
```

Patch dictionary misses according to a specified exception format. Exception slots must contain a valid placeholder dictionary index, such as zero; never dereference an out-of-range sentinel and rely on patching it afterward. Define deterministic dictionary tie breaking. Validate dictionary sizes, indices, split counts, and positions. SIMD work consists of unpacking index and low-bit streams, dictionary lookup, shifting, OR, and stores. Small dictionaries may use register selection or comparison/blend sequences; first compare these with scalar lookup feeding vector reconstruction. Do not assume every backend has cheap gathers or identical permutation instructions.

Keep the format mode-discriminated from the first prototype. Before persisting a public ALP format, either finish RD or document explicitly that the experimental decimal-only version has a distinct version and limited mode set. Do not silently change the interpretation of an existing mode later.

9. **Define the ALP chunk container and timestamp streams.**

Introduce `EncALP` using a newly assigned encoding value, without renumbering existing values or using Head's reserved OOO bit `0x80`. Specify a versioned, endian-independent container in [tsdb/docs/format/chunks.md](tsdb/docs/format/chunks.md). Keep the conventional two-byte sample count at the front. Enforce a hard maximum of 65,535 samples per chunk before append and during decode, consistent with Head's sample-count metadata.

The initial format specification should describe these fields and their exact byte widths before implementation is declared stable:

| Scope | Required information |
| --- | --- |
| Chunk header | Sample count, format version, flags, block count, and packing-layout identification if not implied by version. |
| Block envelope | Actual sample count; timestamp, start-timestamp, and value section boundaries or lengths. |
| Decimal value header | Mode, exponent, factor, signed frame base, bit width, exception count. |
| Decimal payload | Packed residuals, sorted unique exception positions, original 64-bit exception patterns. |
| RD value header/payload | Split size, dictionary and index widths, dictionary entries, both packed streams, exceptions. |
| Raw/constant mode | Actual IEEE-754 bit patterns without arithmetic conversion. |
| Optional block directory | Offsets and last timestamps needed for skipping blocks without decoding values. |

Use a compact fixed decimal header as a starting point: count `u16`, mode `u8`, width `u8`, exponent `u8`, factor `u8`, exception count `u16`, base `i64` totals 16 bytes. Avoid duplicating the count if the block envelope already owns it. Benchmark the complete envelope and timestamp overhead before choosing fixed-length versus variable-length section sizes.

Store exception positions as `u16` initially so the same representation supports the 128- and 1,024-value prototypes. Smaller position encodings can be a measured format decision before freezing. Require sorted, unique positions strictly below the actual count.

Use a separate timestamp delta/delta-of-delta stream with an independent restart at every block. Store start timestamps in their own optional stream, covering absent values, unchanged values, resets, and transitions between zero and nonzero. Reuse proven encoding primitives where their contracts fit; embedding dummy values in XOR2 would retain unnecessary value work and metadata.

Initially decode timestamps and start timestamps with scalar code. This isolates the value SIMD benefit while preserving existing semantics. Benchmark both value-only throughput and complete `(st,t,v)` iteration, since timestamp work can become the next bottleneck.

For multiblock chunks, a directory can allow `Seek(t)` to skip complete blocks. Evaluate its size cost before enabling it for a one-block chunk. Every skipped block must have independent timestamp and value state. Validate section arithmetic, count sums, truncated payloads, unsupported modes, widths, exponents, flags, dictionary references, and exception positions before calling an unchecked hot kernel.

10. **Adapt the scalar iterator to batch decoding.**

Implement `alpIterator` with a block cursor, position inside the decoded block, error state, and reusable timestamp/value/start-timestamp buffers. Begin with capacity 128 for the TSDB candidate, and size separately for the 1,024-value experiment.

The scalar API can still benefit from real SIMD:

```text
Next():
    if decoded samples remain:
        advance the current index
        return ValFloat
    locate and validate the next block
    decode its timestamps and start timestamps
    run a SIMD value kernel for the block
    apply value exceptions
    expose the first decoded sample

At():
    return timestamps[index], values[index]

AtST():
    return startTimestamps[index], or zero for an absent stream
```

No decompression should occur in `At()`. Repeated `At()` calls must return the same values, and iterator reuse must discard stale positions and errors while preserving reusable capacity. Interleaved iterators must own independent decode state.

`Seek(t)` first checks the current sample, then the remaining decoded samples, then skips whole blocks when metadata permits. Decode only the block that may contain the target. Measure instant-query seeks and first-sample latency as well as complete scans; decoding 1,024 values to answer one lookup can erase a throughput win.

Start with the existing `chunkenc.Iterator` interface. An optional batch interface can be a later optimization after profiling the scalar method-call overhead. If added, specify whether returned slices are borrowed, how long they remain valid, how `Next` and `Seek` interact with a batch, and how sample isolation and tombstones limit visible results. Document those rules at the interface definition. Do not make a PromQL-wide batch API rewrite a prerequisite for testing this codec.

Three 128-element arrays occupy roughly 3 KiB per iterator; three 1,024-element arrays occupy roughly 24 KiB, before other state. Avoid allocating the start-timestamp buffer when absent. Measure retained memory across many active iterators and avoid putting large maximum-sized scratch arrays on every active series.

11. **Integrate immutable output before mutable Head.**

The first storage milestone should read and write ALP chunks from complete sample batches in a test-only finalized block writer. Register the decoder and pool support and test serialization and querying. Then introduce an explicit opt-in conversion hook after finalized chunks are collected and before `chunkw.WriteChunks` in `tsdb/compact.go`. Convert eligible float chunks from complete batches, retain histogram chunks, preserve time metadata and every `(st,t,v)` triple, and leave source blocks intact until the normal compaction commit. Keep this writer policy separate from Head's float-selection setting until Head support is ready.

The current compaction encoding callback only influences chunks that get re-encoded: `storage/merge.go` passes ordinary nonoverlapping chunks through unchanged. Changing that callback alone will not create ALP output for normal compaction. The explicit hook makes the immutable-storage experiment testable without changing every ingestion transaction.

Implement an ALP chunk appender that can also reopen a serialized chunk, because that is part of the surrounding chunk contract. Prefer retaining completed blocks and buffering only a trailing partial block. Appending after `Compact`, obtaining a new appender after `Reset`, and calling `Bytes()` between appends must all preserve previous samples.

Before selecting ALP for mutable Head, solve both publication and size accounting:

- Replace the float append path's implicit `len(Bytes())` size probe with an internal, encoding-aware size/capacity policy. Define whether it reports actual serialized bytes or a conservative bound including the pending tail. It must be cheap and must charge pending data.
- Define ALP's maximum block and chunk sizes explicitly. The existing XOR allowance of 19 bytes for the next sample does not describe a block flush, exception list, or added directory entry.
- Store completed block payloads immutably. Appending a new block may allocate a new backing slice; it must not overwrite bytes visible to a running iterator.
- At Head iterator creation, capture a stable block list and snapshot the mutable tail while holding the existing series lock. Capture the sample count and respect `stopIterator`/isolation boundaries. A later append must not change a previously constructed iterator's visible data.
- Make `Bytes()` return a complete, self-contained serialization of all current samples. It can finalize a temporary tail representation and cache it by append generation, but it must not mutate a snapshot used by an existing iterator or repeatedly encode from a hot size check.
- Invalidate serialization caches on append. Distinguish reusable allocation capacity from cached decoded values so benchmarks cannot accidentally skip decoding.
- Treat `Compact()` as an optimization opportunity, not a promise that the chunk can never receive another sample.

Prefer chunk-owned state that survives appender reconstruction; an appender-local-only tail would disappear when callers replace the appender. A read-only mmapped chunk must never be modified in place when reopened for append. Document copy ownership at `Reset`, `Bytes`, and iterator publication boundaries wherever semantics extend existing contracts.

The Head-memory experiment is a release gate. Buffering 128 full triples means about 3 KiB per active series before overhead, which can be unacceptable at high cardinality. Evaluate bounded tail sizes, retaining compact timestamp streams, lazy value allocation, or restricting the first rollout to immutable output. Do not enable Head ALP solely because the scan microbenchmark is faster.

12. **Update all encoding selection and compatibility paths.**

Perform these changes as a tracked integration checklist:

| Files or subsystem | Required work |
| --- | --- |
| `tsdb/chunkenc/chunk.go` | Add the encoding name, validation, constructors, pool support, and read dispatch. Keep old encoding IDs unchanged. |
| `tsdb/chunkenc/chunk_test.go` | Extend existing factory, reset, appender, pool, overflow, and compatibility tables. |
| `tsdb/head.go`, `head_append.go`, `head_append_v2.go` | Replace `useXOR2` as the float-selection mechanism with an actual encoding choice or a focused selector in both appender APIs; preserve the separate histogram-ST setting. Add ALP size policy and publication behavior. |
| `tsdb/db.go`, `config/config.go`, `cmd/prometheus/main.go` | Add explicit `alp` parsing, startup validation, reload behavior, and opt-in gating at the stage where Head support is ready. |
| `storage/series.go`, `storage/merge.go` | Audit new-chunk selection and re-encoding so ALP is not silently converted to XOR by a two-choice boolean. Preserve ST when choosing any fallback. |
| `tsdb/querier.go`, `tsdb/compact.go` | Cover tombstones, chunk rewriting, merges, size accounting, and float-sample statistics. Preserve existing ALP chunks where appropriate. |
| `tsdb/head_wal.go`, `tsdb/head_wal_replay_test.go`, snapshot tests in `tsdb/head_test.go` | Keep the WAL's logical samples unchanged; verify restart with encoded snapshots/mmapped chunks and current selection policy. |
| `tsdb/ooo_head.go`, `tsdb/ooo_head_read.go`, OOO tests | Audit out-of-order chunk creation and merging; make any initial XOR fallback intentional and tested. |
| `tsdb/chunks` and snapshot format readers | Verify ALP round trips, CRC coverage, truncation/corruption errors, pool reuse, and architecture independence. |
| `storage/remote/codec.go`, `prompb/types.proto` | Prevent unsupported ALP chunks from being emitted directly; choose and test wire compatibility behavior. |
| `cmd/promtool/tsdb.go` | Count and report ALP as float samples alongside the existing XOR families. |
| `docs/configuration/configuration.md`, `docs/feature_flags.md`, `tsdb/docs/format/*` | Document configuration scope, the binary format, experimental status, and upgrade/downgrade behavior. Check OpenAPI only if an affected API setting is represented there. |

For initial encoding reloads, conservatively cut the active float chunk when switching between ALP and XOR/XOR2. Let existing chunks retain their original encoding. Do not broaden `CompatibleValues` until the append and ST implications are explicitly proven. Test both directions, with and without ST storage.

For streamed remote read, initially transcode ALP without start timestamps to XOR, and ALP with start timestamps to XOR2. The latter preserves ST but requires an XOR2-capable reader; it does not guarantee compatibility with every historical remote-read client. Document and test that boundary, following the existing deployment requirements for XOR2, and do not silently discard ST. Account for transcoding CPU and possible frame splitting. If native ALP transport is added later, it needs an assigned protobuf enum plus an actual client capability/negotiation strategy; merely adding an enum does not upgrade old clients. Sample-based remote read continues through ordinary iteration and retains its existing protocol semantics.

Reader support must be independent of whether ALP writes are currently enabled and whether SIMD is available. Turning off the writer must not make existing ALP data unreadable. Document that older binaries without the ALP decoder cannot read persisted ALP chunks. Any public rollback procedure must distinguish disabling new ALP writes from converting already persisted data.

13. **Test losslessness, the format, and the complete storage lifecycle.**

Prefer new cases in existing table-driven tests, plus focused primitive tests where no suitable table exists. Cover the following independent properties:

| Test family | Required cases and assertions |
| --- | --- |
| Float round trip | Exact `Float64bits` equality for ordinary decimals, integers, negatives, both zeros, infinities, subnormals, all representative NaN payload classes, and stale NaN. |
| Arithmetic boundaries | Powers of ten, halfway rounding cases, values near conversion limits, products near signed-64-bit limits, values around `2^51` and `2^53`, and scaled values that become non-finite. |
| Frame packing | Every width 0–64, positive/negative/mixed-sign bases, maximum residuals, cross-word extraction, all tail positions, and distinct lane patterns that reveal permutations. |
| Counts | Empty chunk; 1–17 values; 31/32/33; 63/64/65; 119/120; 127/128/129; 239/240; 1,023/1,024/1,025 in the larger-block experiment; 65,535 samples and rejection of a 65,536th append at the chunk API. |
| Exceptions | None, one at every lane/tail boundary, sparse stale values, dense exceptions, all exceptions, and exception positions on both sides of a block boundary. |
| ALP-RD | Every supported split, dictionary sizes and misses, repeated and arbitrary high bits, low-bit boundaries, and exact raw-bit reconstruction. |
| Backend differential | Same bytes decoded by scalar, AVX2, AVX-512, and NEON; identical output bits and error behavior. Call only kernels supported by the test CPU. |
| Iterator behavior | Reuse across encoding types, repeated `At`, ST alignment, `Seek` before/inside/after a block, forward-only behavior, EOF/errors, and simultaneous independent iterators. |
| Mutation/publication | Read after every append; `Bytes` between appends; append after `Compact`; `Reset(Bytes())`; appender reopening; snapshot iterator followed by further appends. |
| TSDB behavior | Both appender APIs, chunk cuts, configuration reloads, WAL replay, memory snapshots, mmap reads, OOO merges, tombstones, compaction, remote read, staleness-sensitive queries, and mixed-encoding blocks. Extend `head_append_v2_test.go` and `db_append_v2_test.go` where applicable. |
| Malformed input | Truncated headers/streams, overflowed lengths, inconsistent counts, invalid modes/widths, invalid RD dictionary indices, invalid exception positions, and missing tail bytes. No panic or out-of-bounds access. |

Fuzz two separate entry points: arbitrary `uint64` patterns interpreted as float64 values for encode/decode round trips, and arbitrary bytes for decode validation. Include tuple-level fuzzing for timestamp and ST alignment. The scalar decoder is the oracle for SIMD, while golden fixtures and, where semantics match, a pinned reference harness provide an independent check on the scalar implementation.

Use the race detector for append/iterator publication and reuse. SIMD decode-ahead may process a physical block that extends beyond a query's isolation boundary, but it must never expose those later samples or read concurrently mutated bytes. Future batch APIs must cap the returned slice at the same visible boundary.

Run targeted package tests during development, then the required project checks before submitting:

```sh
go test ./tsdb/chunkenc ./tsdb/chunks ./tsdb ./storage ./storage/remote ./config
go test ./promql ./cmd/prometheus ./cmd/promtool
go test -race ./tsdb/chunkenc ./tsdb ./storage
GOEXPERIMENT=simd go test ./tsdb/chunkenc ./tsdb ./storage ./storage/remote
go test ./tsdb/chunkenc -run '^$' -fuzz '^FuzzALPRoundTrip$' -fuzztime=60s
go test ./tsdb/chunkenc -run '^$' -fuzz '^FuzzALPDecode$' -fuzztime=60s
make lint
```

The fuzz names are proposed tests. Run the experiment commands with the pinned Go 1.27 toolchain. Execute tests on real amd64 and arm64 machines; cross-compilation alone cannot validate instructions or detect ISA dispatch mistakes. Run ordinary nonexperimental builds on the module's minimum supported version and at least one architecture using the scalar fallback. Add experimental lint coverage if ordinary lint excludes SIMD files.

14. **Measure codec speed, total query cost, and memory separately.**

Extend [benchmark_test.go](tsdb/chunkenc/benchmark_test.go) to include varied values; its current active value pattern is constant. Preserve its useful behaviors of forcing `Bytes()` to charge buffered encoding and resetting from serialized bytes before decoding. Remove the approximate ALP comparison. Benchmark data generation must occur outside timed loops, with deterministic seeds and outputs consumed by a sink.

The benchmark matrix should include:

| Dimension | Cases |
| --- | --- |
| Values | Constant, integral counters, counters with resets, decimal gauges, decimal changes, sign changes, high-precision computed floats, random raw patterns, and real representative metric traces if available. |
| Exceptions | 0%, sparse, 1%, 5%, 20%, and all exceptions, with explicit staleness patterns. |
| Timestamps/ST | Regular scrapes, jitter, gaps, missing ST, cumulative ST, resets, and per-sample ST. |
| Size | Very short chunks, 120, 128, 240, 256, 512, and 1,024 values, including incomplete final blocks. |
| Work | Parameter search, encoding including finalization, residual unpacking, fused value decoding, full chunk iteration, sparse seeks, ingestion, compaction, and representative PromQL queries. |
| Implementation | XOR, XOR2, scalar ALP, staged SIMD ALP, fused SIMD ALP, and ALP-RD. |
| Machine | AVX2-only amd64, AVX-512-capable amd64, arm64 NEON; record CPU, OS, toolchain, selected backend, and experiment settings. |

Report ns/value, values/second, decoded throughput where meaningful, encoded bytes/sample including all metadata, allocations/op, allocated bytes/op, first-sample/seek latency, and retained Head/iterator memory. Include encode parameter-search cost. Separate hot-cache and larger working-set scans. For end-to-end queries, report query latency and CPU, not just the value kernel.

Record a baseline before production changes and compare six runs as required by [AGENTS.md](AGENTS.md). The final comparative benchmark suite should have matching names for the scalar and SIMD runs:

```sh
# Run on the benchmark baseline commit, then on the implementation commit.
go test ./tsdb/chunkenc -run '^$' -count=6 -benchmem \
  -bench 'Benchmark(Appender|Iterator|ALP)' > /tmp/alp-before.txt
go test ./tsdb/chunkenc -run '^$' -count=6 -benchmem \
  -bench 'Benchmark(Appender|Iterator|ALP)' > /tmp/alp-after.txt
benchstat /tmp/alp-before.txt /tmp/alp-after.txt

# Release-build comparison: same source and toolchain, experiment off/on.
go test ./tsdb/chunkenc -run '^$' -count=6 -benchmem \
  -bench 'BenchmarkALP' > /tmp/alp-scalar.txt
GOEXPERIMENT=simd go test ./tsdb/chunkenc -run '^$' -count=6 -benchmem \
  -bench 'BenchmarkALP' > /tmp/alp-simd.txt
benchstat /tmp/alp-scalar.txt /tmp/alp-simd.txt
```

The first two commands are run on different revisions, as their comment states. The experiment-off/on comparison includes compiler-experiment effects. Also compare forced scalar with each supported SIMD backend inside the same experimental binary to isolate kernel improvements. Give those cases explicit benchmark names and include AVX2 versus AVX-512 on the same capable machine. Repeat runs in alternating order on a controlled host if thermal or frequency variation affects results. Use profiles to explain regressions rather than reporting only favorable cases.

Suggested success criteria are decision gates, not promised results:

- All float bits, timestamps, and start timestamps survive round trips and all enabled backends agree.
- Generated machine code demonstrably processes multiple values per instruction in the hot decode loop.
- After warm-up and iterator reuse, regular block decoding has no allocation per value or per block.
- Fused decode improves representative ALP block throughput over the same-format scalar implementation by a meaningful, repeatable margin; use 1.5x as an initial investigation target, not a release guarantee.
- Complete iteration and representative queries show a benefit that survives timestamp processing and scalar iterator calls.
- Compression comparisons include all metadata and real short chunks. Explain or avoid workloads where XOR/XOR2 wins.
- Mutable Head support stays within an agreed measured memory/ingestion budget before it is enabled; immutable-only rollout remains a valid first release.

15. **Deliver the work in independently reviewable stages.**

| Stage | Concrete deliverable | Completion condition |
| --- | --- | --- |
| A. Toolchain and baseline | SIMD capability probe, current-codec benchmarks, bit-exact comparison cleanup, representative data matrix. | Explicit instructions confirmed on target machines; usable baseline saved. |
| B. Scalar value codec | Decimal transform, safe arithmetic, exceptions, raw/constant modes, independent scalar pack/unpack, golden fixtures. | Bit-exact tests and fuzzing pass without TSDB integration. |
| C. Packing decision | 128- and 1,024-value prototypes, compact tails, ISA-independent layout specification. | Size and throughput results justify the chosen initial layout. |
| D. Explicit SIMD | AVX2 emulation, AVX-512 and NEON kernels, CPU dispatch, generated width specializations, fused decode. | Backend differential tests, real-CPU execution, and instruction inspection pass. |
| E. ALP-RD | Adaptive high-precision representation and its tested decode path. | RD round trips and total-cost selection work; supported format modes are fixed. |
| F. Chunk reader/writer | Versioned container, timestamp/ST streams, pooled chunk, batch-backed scalar iterator, seek and malformed-input handling. | Serialized chunks work through existing chunk/storage readers. |
| G. Immutable storage experiment | Controlled compaction/output path, mixed encodings, statistics, restart/query tests, remote-read adapter. | Stored ALP chunks are readable with scalar builds and compatible remote responses. |
| H. Mutable Head | Cheap size policy, stable tail snapshots, appender reconstruction, reload/replay/OOO integration. | Race tests pass and measured memory plus ingestion costs are acceptable. |
| I. Opt-in rollout | Configuration, docs, CI matrix, benchmark analysis, release notes, upgrade/downgrade guidance. | Required checks pass and complete benchmarks support the stated scope. |

Stages B and the storage-lifecycle design can proceed in parallel after A; packing and backend work must share a fixed prototype contract. Do not freeze the persisted format before arithmetic, packing, and backend equivalence are established. Query-level batch consumption, SIMD encoding, and larger persisted chunks are follow-ups driven by profiles.

Each implementation commit should compile and pass its relevant tests. Keep selection refactors separate from codec additions, use `git commit -s`, and use titles such as `tsdb: add lossless ALP value blocks` and `tsdb [PERF]: decode ALP blocks with experimental Go SIMD`. Every PR needs the repository's `release-notes` block; performance PRs include before/after `benchstat` output and explanations for regressions. Link an issue with a closing keyword only when an actual issue is assigned.

The first concrete milestone is a bit-exact scalar codec plus a fused experimental-SIMD decoder benchmarked on 120/128-value and 1,024-value blocks. That proves the requested multiple-values-per-vector behavior and its actual benefit before committing Prometheus to a new persisted encoding or larger Head buffers.
