# Confirmation of variable benchmark rows

The primary matrix retains every measurement. We selected every primary row
whose largest ns/op exceeded 1.5 times its smallest, then repeated all codecs
and both builds for the affected case and operation. Confirmations use six
500 ms repetitions, twice the primary repetition duration. The rule was selected
while the primary run was in progress; it is a variability check, not an
independent preregistered experiment or a license to discard inconvenient data.

The table below lists flagged rows. The complete matched controls and repeats
are in [summary.csv](summary.csv) and [raw/](raw/). Primary and confirmation
measurements are kept separate and are never averaged together.

| Build | Benchmark | Primary max/min | Primary median µs | Confirmation median µs | Confirmation max/min |
| --- | --- | --- | --- | --- | --- |
| scalar | float-histogram/noisy-fractional/n=120/buckets=8/time=regular/ALPv3/decode | 2.37 | 4.635 | 4.173 | 1.50 |
| scalar | float-histogram/bursty/n=1024/buckets=8/time=regular/legacy/decode | 2.01 | 78.677 | 77.521 | 1.44 |
| scalar | float/decimal2/n=120/buckets=0/time=changing-st/ALP/encode | 1.86 | 3.049 | 5.716 | 1.16 |
| scalar | integer-histogram/layout/n=120/buckets=128/time=regular/ALPv4/decode | 1.99 | 31.256 | 49.526 | 1.12 |
| simd | integer-histogram/layout/n=120/buckets=128/time=regular/legacy/decode-float | 1.98 | 54.206 | 48.361 | 1.52 |
| scalar | float/stale5/n=1024/buckets=0/time=no-st/ALP/decode | 1.85 | 3.614 | 3.289 | 1.51 |
| scalar | integer-histogram/bursty/n=120/buckets=128/time=regular/ALPv3/decode-float | 1.95 | 38.806 | 37.907 | 1.29 |
| simd | float-histogram/layout/n=120/buckets=128/time=regular/legacy/encode | 2.09 | 279.779 | 266.712 | 1.49 |
| simd | float-histogram/layout/n=120/buckets=128/time=regular/ALPv4/encode | 2.24 | 555.930 | 518.054 | 1.54 |
| scalar | float/noisy-decimal/n=120/buckets=0/time=no-st/ALP/encode | 2.24 | 2.676 | 3.873 | 1.45 |
| scalar | float/stale5/n=32/buckets=0/time=no-st/ALP/decode | 2.81 | 0.144 | 0.315 | 1.40 |
| scalar | float/counter/n=1024/buckets=0/time=regular/XOR2/decode | 1.68 | 15.364 | 26.528 | 1.12 |
| scalar | float/counter/n=1024/buckets=0/time=regular/ALP/encode | 1.90 | 17.348 | 14.799 | 1.05 |

[Scalar benchstat](scalar-benchstat.txt), [SIMD benchstat](simd-benchstat.txt), and [selection and execution metadata](metadata.json).
