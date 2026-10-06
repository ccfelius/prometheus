# Comprehensive ALP compression benchmark

This report compares the current ALP implementation with XOR, XOR2, and the
existing integer and float histogram encodings. It measures complete encoding,
materialized decoding, CPU time, allocated memory, and stored size, including
experimental histogram v4. It also compares scalar and ARM64 SIMD execution.
The tables identify where faster decoding or smaller output requires more
encoding work; no single aggregate score is treated as a production result.

The run contains **7,728 observations**, **1,288 medians**, and **111 scenarios**,
with six repetitions at 250 ms per operation. It ran on Apple M5 Pro,
macOS-27.0-arm64-arm-64bit, using Go 1.27.1 and source revision `0647ddc2d8c9592b8446b3533d65127293162f4b`.
Started `2026-10-01T10:56:50.475993+00:00`; completed `2026-10-01T12:47:02.866881+00:00`.
Recorded benchmark command durations total 38.7 minutes;
the UTC interval is 110.2 minutes. Commands use a monotonic clock,
while start/end metadata uses UTC. The run does not diagnose any discrepancy
between those totals. Neither total is substituted for per-operation CPU cost.

## Main findings

- **float, ALP versus XOR:** fewer stored bytes in 26/32 scenarios; warm decoding over 5% faster in 30/32; encoding CPU over 5% higher in 25/32.
- **float, ALP versus XOR2:** fewer stored bytes in 54/66 scenarios; warm decoding over 5% faster in 66/66; encoding CPU over 5% higher in 54/66.
- **integer-histogram, ALPv3 versus legacy:** fewer stored bytes in 8/17 scenarios; warm decoding over 5% faster in 17/17; encoding CPU over 5% higher in 17/17.
- **integer-histogram, ALPv4 versus legacy:** fewer stored bytes in 12/17 scenarios; warm decoding over 5% faster in 17/17; encoding CPU over 5% higher in 17/17.
- **float-histogram, ALPv3 versus legacy:** fewer stored bytes in 13/21 scenarios; warm decoding over 5% faster in 21/21; encoding CPU over 5% higher in 21/21.
- **float-histogram, ALPv4 versus legacy:** fewer stored bytes in 20/21 scenarios; warm decoding over 5% faster in 21/21; encoding CPU over 5% higher in 21/21.

For 120 two-decimal float samples, ALP stores 1.308 bytes/sample versus XOR2's 5.775, decodes 2.29× as fast, and uses 1.32× the encoding CPU.

For 128-bucket smooth float histograms, v4 reduces size from 250.10 to 95.71 bytes/snapshot relative to v3, while taking 1.16× the encoding time and 1.83× the decoding time.


ALP's strongest measured advantage is decoding throughput. Decimal floats and
fractional histograms also show substantial storage savings. Encoding generally
costs more CPU than the existing codecs, so these results support a tradeoff
between writing cost, reading cost, and stored size rather than a universal
replacement. Histogram v4 improves the number of cases with storage savings,
but can make decoding slower than v3 and remains experimental. The adaptive
conversion measurements show the separate benefit of retaining an existing
chunk when ALP would not save enough bytes.
These counts compare medians over the measured synthetic cases. A 5% threshold
is a descriptive filter, not a significance test, and the case counts do not
represent production frequencies. The six-run benchstat files provide
statistical comparisons for individual cases.
Their p-values are not adjusted for the many comparisons in this matrix, so
small isolated differences should not drive codec selection.

## Float compression and decompression

SIMD build, 120 samples, regular timestamps and zero start timestamps. XOR
cannot represent start timestamps, so its comparisons only use these compatible
inputs. All codecs receive identical float bits and timestamps. Times cover a
complete 120-sample batch. CPU ns/sample includes user and system process CPU.
Allocated bytes and allocation counts are per encoding operation.

| Pattern | Buckets | Codec | Encoded B/sample | Numeric ratio | Encode µs/batch | Decode µs/batch | Encode CPU ns/sample | Decode CPU ns/sample | Encode allocated B | Encode allocations |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| constant | 0 | XOR | 0.400 | 20.00:1 | 0.684 | 0.320 | 5.68 | 2.67 | 544 | 6 |
| constant | 0 | XOR2 | 0.283 | 28.24:1 | 0.590 | 0.420 | 4.89 | 3.50 | 560 | 6 |
| constant | 0 | ALP | 0.333 | 24.00:1 | 1.044 | 0.272 | 8.47 | 2.27 | 4552 | 7 |
| counter | 0 | XOR | 1.967 | 4.07:1 | 1.472 | 0.753 | 12.21 | 6.28 | 800 | 7 |
| counter | 0 | XOR2 | 1.975 | 4.05:1 | 1.213 | 0.723 | 10.06 | 6.02 | 816 | 7 |
| counter | 0 | ALP | 1.775 | 4.51:1 | 2.398 | 0.480 | 19.73 | 4.00 | 4552 | 7 |
| decimal2 | 0 | XOR | 5.767 | 1.39:1 | 2.116 | 1.006 | 17.51 | 8.39 | 2208 | 9 |
| decimal2 | 0 | XOR2 | 5.775 | 1.39:1 | 1.772 | 0.855 | 14.64 | 7.13 | 2224 | 9 |
| decimal2 | 0 | ALP | 1.308 | 6.12:1 | 2.356 | 0.374 | 19.38 | 3.11 | 4552 | 7 |
| decimal6 | 0 | XOR | 5.733 | 1.40:1 | 2.631 | 1.078 | 21.81 | 8.98 | 2208 | 9 |
| decimal6 | 0 | XOR2 | 5.742 | 1.39:1 | 1.831 | 0.912 | 15.12 | 7.60 | 2224 | 9 |
| decimal6 | 0 | ALP | 1.392 | 5.75:1 | 6.619 | 0.382 | 54.93 | 3.19 | 4552 | 7 |
| noisy-decimal | 0 | XOR | 7.117 | 1.12:1 | 2.424 | 1.026 | 20.07 | 8.56 | 2208 | 9 |
| noisy-decimal | 0 | XOR2 | 7.125 | 1.12:1 | 1.947 | 0.901 | 16.10 | 7.51 | 2224 | 9 |
| noisy-decimal | 0 | ALP | 2.175 | 3.68:1 | 2.392 | 0.388 | 19.69 | 3.23 | 4552 | 7 |
| computed | 0 | XOR | 7.108 | 1.13:1 | 2.604 | 1.071 | 21.59 | 8.93 | 2208 | 9 |
| computed | 0 | XOR2 | 7.117 | 1.12:1 | 2.107 | 0.926 | 17.44 | 7.70 | 2224 | 9 |
| computed | 0 | ALP | 6.742 | 1.19:1 | 4.608 | 0.568 | 38.16 | 4.74 | 4552 | 7 |
| random-finite | 0 | XOR | 8.467 | 0.94:1 | 2.235 | 1.095 | 18.39 | 9.13 | 3616 | 10 |
| random-finite | 0 | XOR2 | 8.475 | 0.94:1 | 2.219 | 0.941 | 18.26 | 7.84 | 3632 | 10 |
| random-finite | 0 | ALP | 7.475 | 1.07:1 | 5.595 | 0.548 | 46.39 | 4.57 | 4552 | 7 |
| random-bits | 0 | XOR | 8.483 | 0.94:1 | 2.276 | 1.112 | 18.73 | 9.27 | 3616 | 10 |
| random-bits | 0 | XOR2 | 8.492 | 0.94:1 | 2.255 | 0.938 | 18.54 | 7.82 | 3632 | 10 |
| random-bits | 0 | ALP | 8.267 | 0.97:1 | 7.246 | 0.307 | 60.16 | 2.56 | 4552 | 7 |
| stale5 | 0 | XOR | 7.700 | 1.04:1 | 2.680 | 1.157 | 22.13 | 9.49 | 3616 | 10 |
| stale5 | 0 | XOR2 | 5.700 | 1.40:1 | 1.897 | 0.871 | 15.60 | 7.26 | 2224 | 9 |
| stale5 | 0 | ALP | 1.808 | 4.42:1 | 2.631 | 0.404 | 21.48 | 3.32 | 4552 | 7 |
| outliers20 | 0 | XOR | 7.442 | 1.07:1 | 2.054 | 1.119 | 16.98 | 9.31 | 2208 | 9 |
| outliers20 | 0 | XOR2 | 7.450 | 1.07:1 | 1.968 | 0.952 | 16.26 | 7.93 | 2224 | 9 |
| outliers20 | 0 | ALP | 7.242 | 1.10:1 | 4.633 | 0.565 | 38.37 | 4.71 | 4552 | 7 |

The numeric ratio is raw numeric payload bytes divided by complete encoded
bytes, including timestamps and headers. For floats the numerator is 8 bytes
per value. It is not a ratio against raw Go structs or a serialized wire message.
Comparing encoded bytes directly avoids ambiguity about the raw baseline.
Stored sizes sum `Chunk.Bytes()` across the batch; outer TSDB storage records,
indexes, and WAL overhead are outside this measurement.

## Timestamp behavior

These 120-sample float batches isolate timestamp jitter and changes to start
timestamps. Only the zero-start-timestamp cases are compared with original XOR.
Encoded size includes the timestamp streams, so value compressibility alone
does not determine the final ratio.

| Pattern | Timestamps | Codec | B/sample | Encode µs/batch | Decode µs/batch |
| --- | --- | --- | --- | --- | --- |
| decimal2 | no-st | XOR | 5.767 | 2.116 | 1.006 |
| decimal2 | no-st | XOR2 | 5.775 | 1.772 | 0.855 |
| decimal2 | no-st | ALP | 1.308 | 2.356 | 0.374 |
| decimal2 | no-st-jitter | XOR | 7.617 | 2.459 | 1.548 |
| decimal2 | no-st-jitter | XOR2 | 7.642 | 2.148 | 1.173 |
| decimal2 | no-st-jitter | ALP | 3.208 | 2.666 | 0.690 |
| decimal2 | regular | XOR2 | 5.800 | 1.864 | 0.897 |
| decimal2 | regular | ALP | 1.375 | 2.462 | 0.380 |
| decimal2 | jitter | XOR2 | 7.667 | 2.308 | 1.220 |
| decimal2 | jitter | ALP | 3.275 | 2.650 | 0.688 |
| decimal2 | changing-st | XOR2 | 8.242 | 2.352 | 2.024 |
| decimal2 | changing-st | ALP | 2.467 | 2.479 | 0.595 |
| computed | no-st | XOR | 7.108 | 2.604 | 1.071 |
| computed | no-st | XOR2 | 7.117 | 2.107 | 0.926 |
| computed | no-st | ALP | 6.742 | 4.608 | 0.568 |
| computed | no-st-jitter | XOR | 8.950 | 2.713 | 1.578 |
| computed | no-st-jitter | XOR2 | 8.958 | 2.260 | 1.320 |
| computed | no-st-jitter | ALP | 8.642 | 4.596 | 1.020 |
| computed | regular | XOR2 | 7.142 | 1.977 | 0.893 |
| computed | regular | ALP | 6.808 | 4.333 | 0.550 |
| computed | jitter | XOR2 | 8.983 | 2.264 | 1.244 |
| computed | jitter | ALP | 8.708 | 4.589 | 0.850 |
| computed | changing-st | XOR2 | 9.575 | 2.484 | 2.189 |
| computed | changing-st | ALP | 7.900 | 4.455 | 0.780 |

## Integer histograms

SIMD build, 120 snapshots, with 8 or 128 total positive and negative buckets.
One sample is a complete histogram snapshot. Bucket values in the integer model
are deltas between adjacent bucket populations; temporal predictors operate
across snapshots. Bursty fixtures add large increments every seventeenth
snapshot and small increments between bursts. They do not model arrivals or
CPU load in the benchmark runner.

The existing codec is `EncHistogramST`. V3 is the configured ALP format; v4
is an explicit experimental format. Encoding includes the mutable histogram
appender and final ALP serialization. Thus these numbers include the existing
legacy encoding stage that ALP histogram appenders still use internally.

| Pattern | Buckets | Codec | Encoded B/sample | Numeric ratio | Encode µs/batch | Decode µs/batch | Encode CPU ns/sample | Decode CPU ns/sample | Encode allocated B | Encode allocations |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| smooth | 8 | legacy | 8.917 | 9.87:1 | 14.977 | 4.990 | 124.60 | 41.58 | 4000 | 16 |
| smooth | 8 | ALPv3 | 5.825 | 15.11:1 | 34.902 | 4.146 | 290.55 | 34.55 | 8192 | 45 |
| smooth | 8 | ALPv4 | 5.642 | 15.60:1 | 37.751 | 4.162 | 314.30 | 34.69 | 8192 | 45 |
| smooth | 128 | legacy | 25.070 | 41.80:1 | 125.168 | 44.684 | 1,043.00 | 372.40 | 11040 | 18 |
| smooth | 128 | ALPv3 | 21.230 | 49.36:1 | 229.704 | 25.951 | 1,913.50 | 216.25 | 26192 | 47 |
| smooth | 128 | ALPv4 | 20.480 | 51.17:1 | 263.338 | 26.099 | 2,194.00 | 217.50 | 26192 | 47 |
| bursty | 8 | legacy | 14.060 | 6.26:1 | 23.154 | 8.464 | 192.70 | 70.53 | 6048 | 17 |
| bursty | 8 | ALPv3 | 20.360 | 4.32:1 | 49.215 | 4.399 | 409.50 | 36.66 | 15552 | 47 |
| bursty | 8 | ALPv4 | 16.270 | 5.41:1 | 57.602 | 4.186 | 479.25 | 34.88 | 12224 | 46 |
| bursty | 128 | legacy | 94.920 | 11.04:1 | 242.656 | 99.710 | 2,019.00 | 831.10 | 49184 | 23 |
| bursty | 128 | ALPv3 | 131.300 | 7.98:1 | 461.070 | 29.558 | 3,836.50 | 246.40 | 103632 | 53 |
| bursty | 128 | ALPv4 | 94.500 | 11.09:1 | 596.703 | 29.605 | 4,937.50 | 246.65 | 83152 | 52 |
| gauge | 8 | legacy | 33.390 | 2.64:1 | 32.205 | 15.203 | 267.55 | 126.70 | 15120 | 257 |
| gauge | 8 | ALPv3 | 26.140 | 3.37:1 | 65.319 | 4.609 | 543.20 | 38.41 | 24752 | 286 |
| gauge | 8 | ALPv4 | 24.930 | 3.53:1 | 73.239 | 4.197 | 609.20 | 34.98 | 24624 | 286 |
| gauge | 128 | legacy | 375.400 | 2.79:1 | 370.387 | 198.822 | 3,072.50 | 1,657.00 | 216208 | 266 |
| gauge | 128 | ALPv3 | 298.400 | 3.51:1 | 667.587 | 31.135 | 5,545.50 | 259.40 | 315712 | 295 |
| gauge | 128 | ALPv4 | 273.000 | 3.84:1 | 771.981 | 28.456 | 6,416.50 | 237.10 | 307520 | 295 |
| resets | 8 | legacy | 9.775 | 9.00:1 | 17.235 | 5.832 | 143.35 | 48.59 | 5872 | 49 |
| resets | 8 | ALPv3 | 10.900 | 8.07:1 | 56.919 | 4.620 | 473.70 | 38.50 | 15472 | 161 |
| resets | 8 | ALPv4 | 9.933 | 8.86:1 | 63.574 | 4.686 | 529.30 | 39.05 | 15344 | 161 |
| resets | 128 | legacy | 29.440 | 35.60:1 | 140.372 | 48.150 | 1,153.50 | 401.10 | 17136 | 53 |
| resets | 128 | ALPv3 | 33.300 | 31.47:1 | 269.203 | 27.262 | 2,239.50 | 227.20 | 59952 | 165 |
| resets | 128 | ALPv4 | 27.300 | 38.39:1 | 304.217 | 27.127 | 2,533.00 | 226.00 | 59440 | 165 |
| layout | 8 | legacy | 9.400 | 10.21:1 | 18.090 | 5.920 | 150.05 | 49.26 | 4968 | 29 |
| layout | 8 | ALPv3 | 7.567 | 12.69:1 | 46.976 | 4.478 | 390.95 | 37.27 | 10744 | 85 |
| layout | 8 | ALPv4 | 7.208 | 13.32:1 | 50.697 | 4.479 | 422.10 | 37.31 | 10712 | 85 |
| layout | 128 | legacy | 27.710 | 38.11:1 | 121.835 | 44.066 | 1,014.50 | 367.15 | 15912 | 33 |
| layout | 128 | ALPv3 | 26.030 | 40.57:1 | 255.650 | 27.463 | 2,101.50 | 229.00 | 40968 | 89 |
| layout | 128 | ALPv4 | 23.620 | 44.71:1 | 290.233 | 25.808 | 2,418.00 | 215.10 | 40584 | 89 |
| stale | 8 | legacy | 11.050 | 7.67:1 | 17.862 | 7.365 | 148.60 | 61.33 | 5648 | 66 |
| stale | 8 | ALPv3 | 20.020 | 4.24:1 | 75.180 | 6.104 | 625.50 | 50.84 | 22528 | 242 |
| stale | 8 | ALPv4 | 17.900 | 4.74:1 | 86.635 | 6.056 | 720.90 | 50.45 | 22240 | 242 |
| stale | 128 | legacy | 36.730 | 27.14:1 | 130.743 | 51.410 | 1,088.50 | 428.05 | 25616 | 78 |
| stale | 128 | ALPv3 | 62.020 | 16.07:1 | 284.717 | 30.413 | 2,368.50 | 253.20 | 97920 | 254 |
| stale | 128 | ALPv4 | 47.230 | 21.11:1 | 333.731 | 30.619 | 2,776.50 | 254.90 | 96256 | 254 |

## Float histograms

The existing codec is `EncFloatHistogramST`. Smooth and bursty fixtures have
integral-valued float bucket populations. Fractional fixtures divide cumulative
bucket counts by 100; noisy fractional fixtures add random fractions. This
separates the favorable temporal case from arbitrary floating-point counts.

The histogram numeric ratio uses count, zero count, sum, and all positive and
negative bucket values at 8 bytes each. It excludes raw timestamps, spans,
custom bounds, struct headers, pointers, and allocation overhead. Stale and
layout-changing snapshots use their actual fixture field counts.

| Pattern | Buckets | Codec | Encoded B/sample | Numeric ratio | Encode µs/batch | Decode µs/batch | Encode CPU ns/sample | Decode CPU ns/sample | Encode allocated B | Encode allocations |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| smooth | 8 | legacy | 20.690 | 4.25:1 | 27.343 | 8.766 | 227.55 | 73.04 | 9072 | 16 |
| smooth | 8 | ALPv3 | 19.020 | 4.63:1 | 51.874 | 2.841 | 431.65 | 23.67 | 16064 | 42 |
| smooth | 8 | ALPv4 | 11.970 | 7.35:1 | 58.340 | 4.036 | 485.60 | 33.63 | 15296 | 42 |
| smooth | 128 | legacy | 177.200 | 5.91:1 | 268.812 | 97.469 | 2,236.00 | 812.30 | 87280 | 23 |
| smooth | 128 | ALPv3 | 250.100 | 4.19:1 | 444.409 | 15.996 | 3,692.00 | 133.35 | 215584 | 51 |
| smooth | 128 | ALPv4 | 95.710 | 10.95:1 | 515.212 | 29.227 | 4,288.00 | 243.55 | 125472 | 49 |
| bursty | 8 | legacy | 20.660 | 4.26:1 | 25.884 | 7.338 | 215.25 | 61.14 | 9072 | 16 |
| bursty | 8 | ALPv3 | 22.760 | 3.87:1 | 50.605 | 2.853 | 421.05 | 23.77 | 16832 | 42 |
| bursty | 8 | ALPv4 | 15.590 | 5.64:1 | 57.917 | 3.954 | 481.55 | 32.95 | 19904 | 43 |
| bursty | 128 | legacy | 169.200 | 6.19:1 | 258.627 | 79.447 | 2,152.00 | 662.00 | 87280 | 23 |
| bursty | 128 | ALPv3 | 230.200 | 4.55:1 | 510.329 | 20.933 | 4,244.50 | 174.35 | 167840 | 50 |
| bursty | 128 | ALPv4 | 204.200 | 5.13:1 | 538.857 | 22.633 | 4,482.50 | 188.60 | 163744 | 50 |
| fractional | 8 | legacy | 79.880 | 1.10:1 | 38.991 | 10.944 | 322.00 | 91.20 | 47216 | 21 |
| fractional | 8 | ALPv3 | 32.190 | 2.73:1 | 80.989 | 2.962 | 671.40 | 24.68 | 63040 | 47 |
| fractional | 8 | ALPv4 | 32.190 | 2.73:1 | 82.791 | 2.962 | 686.50 | 24.69 | 63040 | 47 |
| fractional | 128 | legacy | 943.000 | 1.11:1 | 431.336 | 124.785 | 3,557.50 | 1,040.00 | 517360 | 29 |
| fractional | 128 | ALPv3 | 225.100 | 4.66:1 | 717.652 | 20.116 | 5,938.00 | 167.65 | 663456 | 55 |
| fractional | 128 | ALPv4 | 225.100 | 4.66:1 | 752.273 | 20.095 | 6,228.50 | 167.35 | 663456 | 55 |
| noisy-fractional | 8 | legacy | 79.340 | 1.11:1 | 34.309 | 10.858 | 283.15 | 90.49 | 47216 | 21 |
| noisy-fractional | 8 | ALPv3 | 73.670 | 1.19:1 | 82.407 | 4.343 | 683.05 | 36.19 | 68416 | 47 |
| noisy-fractional | 8 | ALPv4 | 73.670 | 1.19:1 | 82.963 | 4.349 | 687.05 | 36.23 | 68416 | 47 |
| noisy-fractional | 128 | legacy | 940.900 | 1.11:1 | 403.401 | 124.734 | 3,326.00 | 1,039.50 | 517360 | 29 |
| noisy-fractional | 128 | ALPv3 | 832.600 | 1.26:1 | 851.605 | 35.292 | 7,046.00 | 294.05 | 742690 | 55 |
| noisy-fractional | 128 | ALPv4 | 832.600 | 1.26:1 | 849.229 | 35.563 | 7,028.00 | 296.35 | 742690 | 55 |
| gauge | 8 | legacy | 30.620 | 2.87:1 | 26.606 | 8.809 | 220.95 | 73.39 | 15072 | 255 |
| gauge | 8 | ALPv3 | 23.960 | 3.67:1 | 51.514 | 2.751 | 428.05 | 22.93 | 24240 | 281 |
| gauge | 8 | ALPv4 | 23.960 | 3.67:1 | 58.663 | 2.754 | 487.30 | 22.95 | 29616 | 282 |
| gauge | 128 | legacy | 323.600 | 3.24:1 | 264.401 | 101.159 | 2,194.00 | 843.05 | 158816 | 263 |
| gauge | 128 | ALPv3 | 249.400 | 4.20:1 | 463.324 | 22.633 | 3,847.50 | 188.40 | 236688 | 289 |
| gauge | 128 | ALPv4 | 244.800 | 4.28:1 | 492.315 | 23.285 | 4,088.50 | 194.05 | 236688 | 289 |
| resets | 8 | legacy | 24.010 | 3.67:1 | 31.707 | 9.266 | 263.75 | 77.20 | 9264 | 45 |
| resets | 8 | ALPv3 | 18.770 | 4.69:1 | 81.310 | 3.688 | 676.80 | 30.73 | 20208 | 145 |
| resets | 8 | ALPv4 | 14.470 | 6.08:1 | 89.974 | 5.293 | 749.05 | 44.11 | 19696 | 145 |
| resets | 128 | legacy | 213.700 | 4.90:1 | 292.949 | 95.701 | 2,435.50 | 797.45 | 108592 | 69 |
| resets | 128 | ALPv3 | 222.700 | 4.71:1 | 479.875 | 15.664 | 3,989.50 | 130.50 | 175984 | 169 |
| resets | 128 | ALPv4 | 92.230 | 11.36:1 | 556.906 | 28.480 | 4,633.00 | 237.35 | 161136 | 169 |
| layout | 8 | legacy | 22.520 | 4.26:1 | 30.924 | 9.479 | 257.45 | 78.98 | 7656 | 27 |
| layout | 8 | ALPv3 | 20.470 | 4.69:1 | 65.053 | 3.003 | 541.50 | 25.02 | 15944 | 77 |
| layout | 8 | ALPv4 | 13.150 | 7.30:1 | 72.615 | 4.303 | 604.45 | 35.85 | 17096 | 78 |
| layout | 128 | legacy | 186.300 | 5.67:1 | 279.779 | 95.584 | 2,326.50 | 796.55 | 98248 | 41 |
| layout | 128 | ALPv3 | 253.000 | 4.17:1 | 508.656 | 16.676 | 4,227.50 | 138.75 | 193768 | 93 |
| layout | 128 | ALPv4 | 96.880 | 10.90:1 | 555.930 | 30.190 | 4,616.50 | 251.20 | 142568 | 91 |
| stale | 8 | legacy | 24.800 | 3.42:1 | 30.186 | 10.370 | 251.20 | 86.36 | 9328 | 61 |
| stale | 8 | ALPv3 | 22.450 | 3.78:1 | 89.921 | 4.289 | 748.40 | 35.73 | 23488 | 213 |
| stale | 8 | ALPv4 | 21.690 | 3.91:1 | 101.844 | 6.261 | 847.60 | 52.17 | 27872 | 218 |
| stale | 128 | legacy | 211.800 | 4.71:1 | 287.637 | 98.692 | 2,387.50 | 822.20 | 121328 | 96 |
| stale | 128 | ALPv3 | 254.900 | 3.91:1 | 507.757 | 20.121 | 4,219.50 | 167.40 | 231776 | 252 |
| stale | 128 | ALPv4 | 117.400 | 8.49:1 | 594.053 | 34.225 | 4,939.50 | 284.95 | 186976 | 248 |

## Chunk size and very wide histograms

The full data includes 32, 120, and 1,024 samples. These selected results show
whether fixed costs amortize with larger input batches. Reset and layout cases
may create multiple chunks; timings and byte totals include every resulting
chunk, so batch size is not necessarily final physical chunk size.

| Input | Samples | Codec | B/sample | Encode M samples/s | Decode M samples/s | Encode CPU ns/sample |
| --- | --- | --- | --- | --- | --- | --- |
| float/decimal2 | 32 | XOR | 6.031 | 48.04 | 120.55 | 20.62 |
| float/decimal2 | 32 | XOR2 | 6.094 | 58.82 | 133.56 | 16.80 |
| float/decimal2 | 32 | ALP | 2.406 | 19.39 | 222.61 | 50.70 |
| float/decimal2 | 120 | XOR | 5.767 | 56.71 | 119.23 | 17.51 |
| float/decimal2 | 120 | XOR2 | 5.775 | 67.72 | 140.29 | 14.64 |
| float/decimal2 | 120 | ALP | 1.308 | 50.94 | 321.24 | 19.38 |
| float/decimal2 | 1024 | XOR | 5.676 | 58.13 | 117.16 | 17.02 |
| float/decimal2 | 1024 | XOR2 | 8.310 | 51.00 | 55.33 | 19.36 |
| float/decimal2 | 1024 | ALP | 1.206 | 74.05 | 319.05 | 13.46 |
| float/random-bits | 32 | XOR | 8.781 | 44.72 | 102.09 | 22.09 |
| float/random-bits | 32 | XOR2 | 8.812 | 45.81 | 123.03 | 21.55 |
| float/random-bits | 32 | ALP | 9.000 | 10.36 | 346.25 | 95.64 |
| float/random-bits | 120 | XOR | 8.483 | 52.72 | 107.91 | 18.73 |
| float/random-bits | 120 | XOR2 | 8.492 | 53.23 | 127.87 | 18.54 |
| float/random-bits | 120 | ALP | 8.267 | 16.56 | 390.62 | 60.16 |
| float/random-bits | 1024 | XOR | 8.388 | 55.80 | 110.17 | 17.66 |
| float/random-bits | 1024 | XOR2 | 11.020 | 44.15 | 51.84 | 22.33 |
| float/random-bits | 1024 | ALP | 8.229 | 16.20 | 398.37 | 61.45 |
| integer-histogram/bursty | 32 | legacy | 14.160 | 5.05 | 15.25 | 197.95 |
| integer-histogram/bursty | 32 | ALPv3 | 18.160 | 1.78 | 23.80 | 561.55 |
| integer-histogram/bursty | 32 | ALPv4 | 16.120 | 1.59 | 24.05 | 629.90 |
| integer-histogram/bursty | 120 | legacy | 14.060 | 5.18 | 14.18 | 192.70 |
| integer-histogram/bursty | 120 | ALPv3 | 20.360 | 2.44 | 27.28 | 409.50 |
| integer-histogram/bursty | 120 | ALPv4 | 16.270 | 2.08 | 28.67 | 479.25 |
| integer-histogram/bursty | 1024 | legacy | 16.430 | 4.70 | 11.61 | 212.35 |
| integer-histogram/bursty | 1024 | ALPv3 | 19.510 | 2.34 | 27.81 | 426.10 |
| integer-histogram/bursty | 1024 | ALPv4 | 16.020 | 2.03 | 29.03 | 491.95 |
| float-histogram/smooth | 32 | legacy | 23.910 | 3.84 | 13.49 | 259.75 |
| float-histogram/smooth | 32 | ALPv3 | 18.340 | 1.58 | 39.22 | 633.10 |
| float-histogram/smooth | 32 | ALPv4 | 14.120 | 1.42 | 26.56 | 704.70 |
| float-histogram/smooth | 120 | legacy | 20.690 | 4.39 | 13.69 | 227.55 |
| float-histogram/smooth | 120 | ALPv3 | 19.020 | 2.31 | 42.25 | 431.65 |
| float-histogram/smooth | 120 | ALPv4 | 11.970 | 2.06 | 29.73 | 485.60 |
| float-histogram/smooth | 1024 | legacy | 24.340 | 4.71 | 12.88 | 211.75 |
| float-histogram/smooth | 1024 | ALPv3 | 21.460 | 2.51 | 45.18 | 397.20 |
| float-histogram/smooth | 1024 | ALPv4 | 11.550 | 2.26 | 33.06 | 442.15 |

Very wide histograms have 1,031 total buckets, crossing the numeric vector boundary.

| Input | Codec | B/snapshot | Encode µs/batch | Decode µs/batch | Encode CPU ns/snapshot | Allocated B/batch |
| --- | --- | --- | --- | --- | --- | --- |
| integer-histogram | legacy | 146.60 | 926.93 | 339.89 | 7,719.50 | 104736 |
| integer-histogram | ALPv3 | 129.60 | 1,649.47 | 192.31 | 13,739.50 | 208720 |
| integer-histogram | ALPv4 | 134.10 | 1,836.88 | 186.29 | 15,300.00 | 208720 |
| float-histogram | legacy | 1,353.00 | 2,057.14 | 799.83 | 17,127.50 | 706288 |
| float-histogram | ALPv3 | 1,296.00 | 3,537.15 | 149.57 | 29,431.50 | 1049376 |
| float-histogram | ALPv4 | 1,296.00 | 3,376.79 | 145.54 | 28,115.00 | 1049376 |

## SIMD versus scalar

Both builds use the same Go compiler and source. Only `GOEXPERIMENT=simd`
differs; the scalar build selects ALP's scalar fallback kernels. This measures
complete-codec effects of NEON, including scalar work
around vector kernels; it is not an isolated instruction benchmark. No AVX2
or AVX-512 performance is inferred from this ARM64 machine.

| Input | Buckets | Codec | Operation | Scalar µs/batch | SIMD µs/batch | SIMD speedup | SIMD CPU/scalar CPU |
| --- | --- | --- | --- | --- | --- | --- | --- |
| float/decimal2 | 0 | ALP | encode | 2.529 | 2.356 | 1.07× | 0.93× |
| float/decimal2 | 0 | ALP | decode | 0.399 | 0.374 | 1.07× | 0.94× |
| float/computed | 0 | ALP | encode | 4.483 | 4.608 | 0.97× | 1.03× |
| float/computed | 0 | ALP | decode | 0.574 | 0.568 | 1.01× | 0.99× |
| float/random-bits | 0 | ALP | encode | 7.233 | 7.246 | 1.00× | 1.00× |
| float/random-bits | 0 | ALP | decode | 0.331 | 0.307 | 1.08× | 0.93× |
| integer-histogram/smooth | 128 | ALPv3 | encode | 226.505 | 229.704 | 0.99× | 1.01× |
| integer-histogram/smooth | 128 | ALPv3 | decode | 30.908 | 25.951 | 1.19× | 0.84× |
| integer-histogram/bursty | 128 | ALPv4 | encode | 576.053 | 596.703 | 0.97× | 1.04× |
| integer-histogram/bursty | 128 | ALPv4 | decode | 33.746 | 29.605 | 1.14× | 0.88× |
| float-histogram/smooth | 128 | ALPv3 | encode | 477.990 | 444.409 | 1.08× | 0.93× |
| float-histogram/smooth | 128 | ALPv3 | decode | 23.178 | 15.996 | 1.45× | 0.69× |
| float-histogram/smooth | 128 | ALPv4 | encode | 575.689 | 515.212 | 1.12× | 0.89× |
| float-histogram/smooth | 128 | ALPv4 | decode | 36.675 | 29.227 | 1.25× | 0.80× |
| float-histogram/fractional | 128 | ALPv3 | encode | 787.740 | 717.652 | 1.10× | 0.91× |
| float-histogram/fractional | 128 | ALPv3 | decode | 23.069 | 20.116 | 1.15× | 0.87× |

## Iterator allocation and histogram conversion

Fresh-iterator decoding creates iterator/output state for each scan of already
resident bytes. It does not flush CPU caches or read from disk. The separate
`decode-float` operation reads integer histograms as floating-point histograms,
including bucket conversion and materialization.

| Input | Buckets | Time | Codec | Operation | Decode µs/batch | CPU ns/sample | Allocated B | Allocations |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| float-histogram/smooth | 8 | regular | ALPv3 | decode-cold | 5.880 | 48.20 | 12896 | 15 |
| float-histogram/smooth | 8 | regular | ALPv4 | decode-cold | 7.072 | 58.14 | 12256 | 16 |
| float-histogram/smooth | 8 | regular | legacy | decode-cold | 9.643 | 80.31 | 784 | 15 |
| float/decimal2 | 0 | no-st | ALP | decode-cold | 0.507 | 4.14 | 1552 | 4 |
| float/decimal2 | 0 | no-st | XOR | decode-cold | 1.041 | 8.67 | 128 | 2 |
| float/decimal2 | 0 | no-st | XOR2 | decode-cold | 0.896 | 7.46 | 144 | 2 |
| float/decimal2 | 0 | regular | ALP | decode-cold | 0.515 | 4.20 | 1552 | 4 |
| float/decimal2 | 0 | regular | XOR2 | decode-cold | 0.940 | 7.83 | 144 | 2 |
| float/random-bits | 0 | no-st | ALP | decode-cold | 0.418 | 3.41 | 1296 | 3 |
| float/random-bits | 0 | no-st | XOR | decode-cold | 1.143 | 9.52 | 128 | 2 |
| float/random-bits | 0 | no-st | XOR2 | decode-cold | 0.975 | 8.12 | 144 | 2 |
| float/random-bits | 0 | regular | ALP | decode-cold | 0.410 | 3.34 | 1296 | 3 |
| float/random-bits | 0 | regular | XOR2 | decode-cold | 0.981 | 8.17 | 144 | 2 |
| integer-histogram/bursty | 128 | regular | ALPv3 | decode-float | 30.866 | 257.25 | 0 | 0 |
| integer-histogram/bursty | 128 | regular | ALPv4 | decode-float | 30.936 | 257.80 | 0 | 0 |
| integer-histogram/bursty | 128 | regular | legacy | decode-float | 105.856 | 881.50 | 16 | 2 |
| integer-histogram/bursty | 8 | regular | ALPv3 | decode-float | 4.362 | 36.35 | 0 | 0 |
| integer-histogram/bursty | 8 | regular | ALPv4 | decode-float | 4.128 | 34.41 | 0 | 0 |
| integer-histogram/bursty | 8 | regular | legacy | decode-float | 8.799 | 73.32 | 16 | 2 |
| integer-histogram/smooth | 128 | regular | ALPv3 | decode-float | 27.211 | 226.80 | 0 | 0 |
| integer-histogram/smooth | 128 | regular | ALPv4 | decode-float | 27.160 | 226.35 | 0 | 0 |
| integer-histogram/smooth | 128 | regular | legacy | decode-float | 48.964 | 408.10 | 16 | 2 |
| integer-histogram/smooth | 8 | regular | ALPv3 | decode-cold | 6.407 | 53.19 | 4272 | 16 |
| integer-histogram/smooth | 8 | regular | ALPv3 | decode-float | 3.983 | 33.19 | 0 | 0 |
| integer-histogram/smooth | 8 | regular | ALPv4 | decode-cold | 6.465 | 53.66 | 4272 | 16 |
| integer-histogram/smooth | 8 | regular | ALPv4 | decode-float | 4.051 | 33.76 | 0 | 0 |
| integer-histogram/smooth | 8 | regular | legacy | decode-cold | 5.910 | 49.22 | 880 | 15 |
| integer-histogram/smooth | 8 | regular | legacy | decode-float | 5.140 | 42.84 | 16 | 2 |

## Adaptive compaction conversion

These measurements begin with an already encoded source chunk and reuse one
encoder for repeated chunks from a homogeneous series. They exclude initial
source encoding. Adaptive rejection includes amortized backoff and periodic
retries; it is not a cold first-decision latency. Forced conversion always
returns an ALP candidate, even if larger. Adaptive conversion requires at least
5% complete-size savings. Histogram adaptive conversion targets v3, not v4.
Throughput here counts input samples considered, including samples deliberately
skipped by rejection hints; it must not be read as a full decoding rate.

| Input | Buckets | Policy | Convert µs/batch | CPU ns/sample | Retained B/sample | Accepted % | Allocated B | Allocations |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| float-histogram/fractional | 8 | adaptive | 26.069 | 216.95 | 23.660 | 100.0 | 3793 | 11 |
| float-histogram/fractional | 8 | forced | 25.886 | 215.60 | 23.660 | 100.0 | 3793 | 11 |
| float-histogram/noisy-fractional | 128 | adaptive | 423.597 | 3,517.00 | 832.600 | 100.0 | 222963 | 12 |
| float-histogram/noisy-fractional | 128 | forced | 423.058 | 3,514.50 | 832.600 | 100.0 | 222963 | 12 |
| float/computed | 0 | adaptive | 0.116 | 0.97 | 7.142 | 0.0 | 16 | 0 |
| float/computed | 0 | forced | 4.619 | 38.45 | 6.808 | 100.0 | 1136 | 3 |
| float/decimal2 | 0 | adaptive | 2.356 | 19.59 | 1.375 | 100.0 | 944 | 3 |
| float/decimal2 | 0 | forced | 2.346 | 19.52 | 1.375 | 100.0 | 944 | 3 |
| float/random-bits | 0 | adaptive | 0.065 | 0.54 | 8.517 | 0.0 | 16 | 0 |
| float/random-bits | 0 | forced | 7.525 | 62.69 | 8.333 | 100.0 | 1264 | 3 |
| integer-histogram/bursty | 128 | adaptive | 6.870 | 57.07 | 94.920 | 0.0 | 3504 | 9 |
| integer-histogram/bursty | 128 | forced | 180.315 | 1,501.50 | 131.300 | 100.0 | 19972 | 11 |
| integer-histogram/smooth | 8 | adaptive | 17.059 | 142.10 | 5.825 | 100.0 | 1392 | 11 |
| integer-histogram/smooth | 8 | forced | 16.733 | 139.40 | 5.825 | 100.0 | 1392 | 11 |

## CPU interpretation

Median process utilization across all rows is
99.9% of one core. A saturated
serial loop can show similar utilization for both a fast and a slow codec.
Compare CPU nanoseconds per sample to compare the work required. Multiply that
number by 0.001 for CPU seconds per million samples. For histograms, a sample
means the complete snapshot, not one bucket.

CPU accounting uses user plus system process CPU and includes runtime/GC work
in the measured process. `GOMAXPROCS=1`, `GOGC=100`, and `-test.cpu=1` are fixed.
Allocation metrics are allocated bytes and allocation counts per batch, not
retained heap, peak RSS, energy, or whole-system CPU utilization. Timing and CPU
cost are related measurements, not independent evidence of a speedup.

Six separate five-second SIMD profiling passes provide diagnostic samples for
selected encoders and decoders. They ran on 2026-10-06 after the primary matrix;
their timings are excluded from the medians above. See the
[CPU hotspot tables and profiles](profiles/summary.md). Flat samples identify
work inside a function, while cumulative samples include its callees.
Some profiles attribute most samples to runtime functions such as `runtime.kevent`
(85.27% in the XOR2 encoding profile). The reason for that attribution was not
established, so these samples do not support precise codec hotspot conclusions.
The quantitative CPU comparisons above use process user/system accounting.

## Repetition variability

The primary matrix is preserved in full. 13
rows had a slowest repetition more than 1.5 times the fastest. We repeated all
codecs and both builds for those affected case/operation pairs at 500 ms per
repetition, producing 396 additional measurements.
These confirmations are kept separate from the 7,728 primary observations.
Use the [confirmation report](confirmation/report.md) when interpreting the
flagged cases; wide variation limits the precision of a headline median.
Confirmation did not eliminate all variability, and several medians shifted
substantially. Keep those cases provisional; the report does not substitute
confirmation medians into the primary tables or discard either set of results.

## Coverage and limits

Fixture generation is deterministic with seed 20260929. Regular timestamps are
15 seconds apart; jitter adds a uniformly chosen offset from minus 1,000 to
plus 1,000 milliseconds. Changing start timestamps advance every 17 samples.
All generation and correctness checks are outside the timed loops.

| Float pattern | Generated values |
| --- | --- |
| constant | Always 42 |
| counter | Integer values beginning at 100,000 with increments of 13 |
| decimal2 | Sequential integers beginning at 100,000 divided by 100 |
| decimal6 | Sequential integers beginning at 100,000 divided by one million |
| noisy-decimal | Random integers within a 10,000-value range divided by 100 |
| computed | `1 + sin(i)/10` |
| random-finite | Random finite values between minus 1,000 and 1,000 |
| random-bits | Arbitrary 64-bit float bit patterns, including special values |
| stale5 | The two-decimal series with a stale NaN every twentieth sample |
| outliers20 | The two-decimal series with every fifth value replaced by `pi*(i+1)` |

Histogram smooth populations increase by `1 + bucketIndex%7` per snapshot.
Bursty populations increase by 1–1,000 every seventeenth snapshot and by 0–2
otherwise. Gauge populations are independently redrawn from 1–10,000. Reset
fixtures clear populations every thirtieth snapshot; layout fixtures add two
buckets halfway through; stale fixtures replace every twentieth snapshot with
a stale histogram. Fractional and noisy fractional formulas are described in
the float histogram section and preserved exactly in the source generator.

- 104 full-codec scenarios and 7 incremental conversion scenarios.
- Ten float patterns at 32, 120, and 1,024 samples, both with and without start
  timestamps; additional timestamp jitter and changing start timestamp cases.
- Integer and float histograms with 8 and 128 buckets; smooth, bursty, gauge,
  reset, layout-change, and stale patterns; fractional and noisy fractional
  float histograms; selected size variations and 1,031-bucket cases.
- All input fixtures passed scalar and SIMD correctness checks before timing.
  Those checks compare decoded float bits, histogram values, timestamps, start
  timestamps, and deterministic encoded output. The preflight one-iteration
  enumeration is excluded from timed results.
- Source and compiler are frozen in two compiled test binaries. Each scenario
  runs serially, scalar/SIMD order alternates, and scenario order uses a fixed
  shuffle seed. Binary hashes, source hash, runner hash, commands, process CPU
  and wall times are recorded.
- These are synthetic memory-resident codec benchmarks on one Apple ARM64
  machine. They do not measure ingestion, WAL, compaction I/O, PromQL execution,
  multi-core scaling, production workload frequency, or sustained server power.
  Processes were not pinned to a specific core and the host was not reserved
  exclusively; background activity and scheduling can affect small differences.
- Comparison against the existing codecs uses the current source revision.
  This is not a before/after implementation comparison; that is available in
  the [optimization report](../alp-encoding-improvements-20260930/report.md).
- V4 remains experimental and opt-in. New readers support it, but older readers
  do not. Compression wins alone do not justify making it the default.

## Data and reproduction

[Interactive results](explorer.html), [all medians CSV](summary.csv),
[all medians JSON](summary.json), [codec comparisons CSV](comparisons.csv),
[raw observations](results.json), [process diagnostics](processes.json),
[run metadata](metadata.json), and [raw process output](raw/) are preserved.

Statistical comparisons are available for
[SIMD float codecs](simd-floats-benchstat.txt),
[scalar float codecs](scalar-floats-benchstat.txt),
[direct SIMD XOR2 versus ALP](simd-xor2-alp-benchstat.txt),
[direct scalar XOR2 versus ALP](scalar-xor2-alp-benchstat.txt),
[SIMD histogram codecs](simd-histograms-benchstat.txt),
[scalar histogram codecs](scalar-histograms-benchstat.txt), and
[scalar versus SIMD](scalar-simd-benchstat.txt).
Float codec tables use XOR as the reference where available; XOR2 and ALP
also have absolute measurements for start-timestamp cases where XOR is absent.
Histogram tables use the existing codec as the reference. Ratios of geometric
means over a heterogeneous matrix should not be treated as production speedups.

```sh
GOTOOLCHAIN=go1.27.1 GOEXPERIMENT='' go test -c ./tsdb/chunkenc -o /tmp/alp-big-scalar.test
GOTOOLCHAIN=go1.27.1 GOEXPERIMENT=simd go test -c ./tsdb/chunkenc -o /tmp/alp-big-simd.test
GOMAXPROCS=1 /tmp/alp-big-scalar.test -test.run='^TestALPMatrixFixtures$'
GOMAXPROCS=1 GOGC=100 /tmp/alp-big-simd.test -test.run='^TestALPMatrixFixtures$' -test.bench='^BenchmarkALPMatrix(Transcode)?$' -test.benchmem -test.benchtime=1x -test.count=1 -test.cpu=1 > /tmp/alp-big-smoke.txt
python3 tsdb/docs/benchmarks/alp-extensive-20260929/run.py --scalar /tmp/alp-big-scalar.test --simd /tmp/alp-big-simd.test --smoke /tmp/alp-big-smoke.txt --out tsdb/docs/benchmarks/alp-comprehensive-20261001 --count 6 --benchtime 250ms
python3 tsdb/docs/benchmarks/alp-comprehensive-20261001/confirm.py --scalar /tmp/alp-big-scalar.test --simd /tmp/alp-big-simd.test
python3 tsdb/docs/benchmarks/alp-comprehensive-20261001/profile.py --binary /tmp/alp-big-simd.test
python3 tsdb/docs/benchmarks/alp-comprehensive-20261001/analyze.py
python3 tsdb/docs/benchmarks/alp-comprehensive-20261001/benchstat.py
```

The report generator reuses the established matrix summarizer. All exact
scenario definitions are in `tsdb/chunkenc/alp_matrix_bench_test.go`.
