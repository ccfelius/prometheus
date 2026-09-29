# ALP histogram benchmark

Measured 2026-09-29, using codec revision `5b5b11813f69474b1621c00fb47101637abf298a`, Apple M5 Pro, darwin/arm64, Go 1.27.1, `GOEXPERIMENT=simd` (NEON), `GOMAXPROCS=1`, `GOGC=100`. Each of ten cases ran six repetitions with a one-second target.

XOR2 is a float-sample codec and cannot encode histograms. Integer histograms are compared with `HistogramST`; floating-count histograms are compared with `FloatHistogramST`. The chunk contains 120 histograms. Values below are medians, with encoded sizes including metadata, timestamps, and start timestamps.

| Family / codec | Bytes/histogram | Space saving vs legacy | Legacy bytes / candidate bytes | Encode µs/chunk | Decode µs/chunk |
| --- | ---: | ---: | ---: | ---: | ---: |
| integer / legacy | 8.8 | 0.0% | 1.000× | 13.514 | 4.954 |
| integer / ALPv1 | 10.68 | -21.4% | 0.824× | 33.076 | 3.716 |
| integer / ALPv2 | 6.942 | 21.1% | 1.268× | 33.543 | 4.039 |
| float / legacy | 19.39 | 0.0% | 1.000× | 26.084 | 8.742 |
| float / ALPv1 | 17.27 | 10.9% | 1.123× | 53.076 | 3.713 |

The size ratio uses the existing encoded histogram format as its reference, not an assumed raw Go struct size. Slice headers, pointed-to arrays, shared metadata, and timestamp storage make a Go struct memory-size comparison a different measurement. A ratio below 1 means the ALP output is larger.

Integer v2 is about 21% smaller than legacy and 35% smaller than ALP v1. Integer v1 is about 21% larger than legacy. Float ALP is about 11% smaller. The complete bytes are compared in adaptive compaction; these gains are specific to this fixture.

## CPU cost

| Family / codec | Encode CPU seconds / million histograms | Encode CPU / legacy | Decode CPU seconds / million histograms | Decode CPU / legacy |
| --- | ---: | ---: | ---: | ---: |
| integer / legacy | 0.11255 | 1.00× | 0.04131 | 1.00× |
| integer / ALPv1 | 0.27527 | 2.45× | 0.03103 | 0.75× |
| integer / ALPv2 | 0.27978 | 2.49× | 0.03395 | 0.82× |
| float / legacy | 0.22009 | 1.00× | 0.07301 | 1.00× |
| float / ALPv1 | 0.44105 | 2.00× | 0.03093 | 0.42× |

CPU time is child-process user + system time measured with Python `resource.getrusage`, normalized by the sum of iterations × 120 histograms. This includes runtime, GC, process setup, and benchmark setup; compilation and analysis are excluded. CPU ratios are aggregate measurements, not confidence intervals. Both codecs keep approximately one core busy under saturation.

## What is measured

The benchmark is [`BenchmarkALPHistograms`](../../../chunkenc/alp_histogram_test.go#L274). Input generation happens before timing. Writing includes construction, 120 appends, and final serialization. Reading scans all samples and materializes each histogram through `AtHistogram` or `AtFloatHistogram` using a reused output object. This is not a count-only scan, a cold-cache experiment, or a PromQL query.

The fixtures are [`GenerateTestHistograms` and `GenerateTestFloatHistograms`](../../../tsdbutil/histogram.go). Each sample has:

- Schema 1 and zero threshold 0.001.
- Four positive and four negative buckets, with a fixed span layout.
- Count `12 + 9*i`, zero count `2 + i`, and sum `18.4 * (i+1)`.
- Counter semantics, with no resets after the initial sample.
- Regular 15-second timestamps and a constant start timestamp of 1.

The integer representation stores bucket deltas; the float representation stores absolute bucket counts. The generated floating counts are whole-number values represented as `float64`, not fractional counts. The fixture does not cover production traces, changing layouts, stale histograms, gauge histograms, large bucket counts, or fractional histogram counts. Correctness tests cover more cases than this performance benchmark.

## Version and implementation implications

Explicit `histograms: alp` writes version 1. Adaptive `histograms: auto` uses integer version 2 candidates and keeps legacy output unless ALP saves at least 5%. Version 2 changes integer numeric vectors from 1,024 values to 128, without changing predictor semantics. It requires a v2-aware reader. Floating-count histograms remain version 1.

The measured write includes the current mutable legacy histogram appender followed by ALP serialization. Direct compaction transcoding avoids rebuilding that mutable representation, but still has source decoding cost. Therefore this write benchmark is not a compaction-throughput measurement.

## Raw data and reproduction

[Measurement script](run.py), [per-repetition and CPU results](results.json), [legacy output](legacy.txt), [ALP v1 output](ALPv1.txt), [ALP v2 output](ALPv2.txt), [benchstat comparison](benchstat.txt). Aggregate output names are normalized to match corresponding integer/float cases in benchstat.

```sh
mkdir -p /tmp/alp-histograms-20260929-095803
GOTOOLCHAIN=go1.27.1 GOEXPERIMENT=simd go test -c -o /tmp/alp-xor2-benchmark.test ./tsdb/chunkenc
python3 tsdb/docs/benchmarks/alp-histograms-20260929/run.py
GOTOOLCHAIN=go1.27.1 go run golang.org/x/perf/cmd/benchstat@v0.0.0-20260908200009-22c9c6c9d4da \
  /tmp/alp-histograms-20260929-095803/legacy.txt \
  /tmp/alp-histograms-20260929-095803/ALPv1.txt \
  /tmp/alp-histograms-20260929-095803/ALPv2.txt
```

For the complete code walkthrough, see the [implementation report](../../alp-implementation-details.md).
