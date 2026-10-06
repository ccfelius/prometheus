# ALP: summary of the last five questions

This summarizes the five questions preceding the request for this document.
Measurements come from the [comprehensive benchmark report](report.md).
Unless specified otherwise, examples use the SIMD build and 120 samples.
For histograms, one sample means an entire histogram snapshot, including its
buckets. Histogram baselines are the existing histogram codecs; XOR and XOR2
are the baselines for ordinary floats.

## 1. Where was ALP significantly slower?

The main disadvantage was encoding cost, especially for small chunks, values
that need more decimal-plan searching or exceptions, and histograms that split
frequently around stale samples.

| Workload | Baseline encoding time | ALP encoding time | ALP time / baseline |
| --- | ---: | ---: | ---: |
| Six-decimal floats, 120 samples | XOR2: 1.831 µs | 6.619 µs | 3.61× |
| Arbitrary float bits, 120 samples | XOR2: 2.255 µs | 7.246 µs | 3.21× |
| Integer histograms, 8 buckets, stale workload | Existing codec: 17.862 µs | v4: 86.635 µs | 4.85× |

Times cover encoding the complete batch, including final serialization.
ALP performs conversion, plan selection, packing, and exception handling.
The histogram writer also still builds through the existing histogram appender
before final ALP serialization. V4 adds predictor and alternative-format trials.
These are implementation costs, not percentages established by CPU profiling.

Decoding is not always faster either. With a fresh iterator, smooth 8-bucket
integer histograms took 6.407 µs with v3 versus 5.910 µs with the existing codec,
about 8% longer. Smooth 128-bucket float histograms decoded in 29.227 µs with
v4 versus 15.996 µs with v3: v4 took 1.83× as long, although both beat the
existing codec's 97.469 µs.

## 2. How was this benchmarked?

- **Environment:** Apple M5 Pro ARM64, Go 1.27.1, `GOMAXPROCS=1`, `GOGC=100`.
  Both scalar and SIMD builds were measured.
- **Coverage:** 111 scenarios, six repetitions at 250 ms per operation,
  producing 7,728 primary observations. Selected variable results received
  longer confirmation runs, recorded separately.
- **Inputs:** Deterministic synthetic floats at 32, 120, and 1,024 samples;
  integer and float histograms, mainly with 8 or 128 buckets, plus selected
  1,031-bucket cases. Patterns included smooth growth, bursts, gauges, resets,
  layout changes, stale markers, fractional values, and irregular timestamps.
- **Encoding:** Create a chunk, append the complete batch, and finalize its
  bytes. Retain every output chunk when the input causes splitting.
- **Decoding:** Scan all samples from resident encoded bytes. Warm scans reuse
  iterator/output buffers; fresh-iterator scans include their setup costs.
  Neither measures cold disk reads.
- **Compression:** Total serialized chunk bytes divided by input samples,
  including chunk headers and timestamps. This excludes outer TSDB records,
  indexes, and WAL storage.
- **CPU:** Process user plus system CPU time from `getrusage`, divided by input
  samples. Less CPU time per sample means less CPU work for the same data;
  it does not imply a busy benchmark loop shows lower CPU utilization.
- **Correctness:** Fixture generation and round-trip validation happen outside
  timing; validation includes float bit patterns and histogram semantics.

These are codec microbenchmarks on one machine, not production ingestion or
query benchmarks. The host was not exclusively reserved, so small differences
and flagged variable results need caution. Comparisons with original XOR only
use inputs without start timestamps, which XOR cannot represent.

## 3. Where was ALP much better?

The strongest results combined faster decoding with smaller chunks, particularly
for decimal floats and several histogram workloads.

| Workload | ALP version | Baseline | Decode speedup | Fewer stored bytes |
| --- | --- | --- | ---: | ---: |
| Two-decimal floats, 120 samples | Float ALP | XOR2 | 2.29× | 77% |
| Fractional float histograms, 128 buckets | v3 | Existing float-histogram codec | 6.20× | 76% |
| Integer gauge histograms, 128 buckets | v4 | Existing integer-histogram codec | 6.99× | 27% |
| Smooth float histograms, 128 buckets | v4 | Existing float-histogram codec | 3.33× | 46% |

These read and storage gains can come with more encoding work. For example,
the fractional float-histogram case used 1.67× the encoding CPU, and the integer
gauge case used 2.09×.

Larger batches can amortize ALP's setup costs. For 1,024 two-decimal floats,
ALP decoded 5.77× faster than XOR2, used about 85% fewer bytes, and used about
30% less encoding CPU. Against original XOR, its decoding gain was about 2.72×
and its storage reduction about 79%. The distinction matters because XOR2 has
additional overhead in this larger-chunk workload, described below.

## 4. What changed between v3 and v4?

These are **histogram format versions**. Ordinary float ALP and the embedded
timestamp/sum stream remain version 1.

V3 packs four reset hints into each byte and compresses the first integer
histogram snapshot instead of storing all its fields as raw 64-bit values.
V4 retains those improvements and adds the following:

| Component | V3 | V4 |
| --- | --- | --- |
| Integer prediction | Fixed delta-of-delta per field | Chooses delta or delta-of-delta per block based on encoded size |
| Integer outliers | Raw or fixed-width packed blocks | Also supports narrow packing with sparse exact exceptions |
| Signed bucket fields | Applies zigzag before temporal prediction | Predicts signed bucket-delta bits first, then zigzags residuals |
| Float histogram fields | Ordinary ALP on numeric values | Also tries exact integer differences between successive values of the same field |

For example, exactly convertible float bucket values such as `100.00`,
`100.01`, and `100.02` can become scaled integers `10000`, `10001`, and `10002`.
V4 can store the initial integer and small differences. It performs prediction
after exact integer conversion, avoiding floating-point subtraction, and uses
the temporal alternative only when it is smaller than ordinary ALP.

Both versions support SIMD. V4 is not the switch that enables SIMD. Both retain
128-value integer blocks and 1,024-value float blocks, and both are lossless.
V4 falls back to ordinary ALP when exact temporal conversion is unsuitable.

For smooth 128-bucket float histograms, v4 reduced storage from **250.10 to
95.71 bytes/snapshot**, about **62% smaller than v3**. The tradeoff was **1.16×
the encoding time** and **1.83× the decoding time**. V4 does not improve every
dataset; fractional histograms used the same 225.10 bytes/snapshot in both.

Configuration-selected ALP histogram writers use **v3**. V4 remains an explicit
experiment through `ALPEncoder.RecodeHistogramV4`; older readers cannot read it.
See the [histogram format specification](../../format/alp_histograms.md).

## 5. Why is XOR2 better than original XOR?

XOR2 improves some cases, but is not universally better. Both codecs compress
float values by XORing their bits with the preceding value. XOR2 changes the
control encoding and adds optional start timestamps.

| XOR2 change | Benefit or tradeoff |
| --- | --- |
| Joint timestamp/value control | Regular timestamps with an unchanged value need 1 bit/sample after initialization, versus XOR's 2 bits. |
| Whole-byte timestamp control/delta fields | Reduces individual bit-writing operations, but some timestamp differences need more space. |
| Dedicated stale-marker encoding | Avoids treating stale NaNs as ordinary float changes. |
| Optional start timestamps | Adds functionality original XOR lacks, with additional format overhead. |

For 120 samples with regular timestamps and no start timestamps:

| Pattern | Bytes/sample, XOR → XOR2 | Encode µs/batch, XOR → XOR2 | Decode µs/batch, XOR → XOR2 |
| --- | ---: | ---: | ---: |
| Constant | 0.400 → 0.283 | 0.684 → 0.590 | 0.320 → 0.420 |
| Two-decimal values | 5.767 → 5.775 | 2.116 → 1.772 | 1.006 → 0.855 |
| Six-decimal values | 5.733 → 5.742 | 2.631 → 1.831 | 1.078 → 0.912 |
| Stale-marker workload | 7.700 → 5.700 | 2.680 → 1.897 | 1.157 → 0.871 |

For ordinary changing floats in these cases, XOR2 mainly improves speed;
compression is essentially unchanged apart from its extra header byte.
Constants and stale markers offer space savings, but constant-value decoding
was slower.

The current XOR2 format also starts carrying start-timestamp information around
its 127-sample threshold, even for the zero-start-timestamp workload. This
contributes to worse results than original XOR in some 1,024-sample cases.
See the [XOR2 format documentation](../../format/chunks.md#xor2-chunk-data).
ALP should therefore be assessed against **both XOR and XOR2**.
