"""Generate the narrative result tables from the saved benchmark medians."""
from pathlib import Path
import json
import statistics

root = Path(__file__).resolve().parent
rows = json.loads((root / 'summary.json').read_text())
comparisons = json.loads((root / 'comparisons.json').read_text())
index = {(r['backend'], r['suite'], r['family'], r['pattern'], r['samples'], r['buckets'], r['timing'], r['codec'], r['operation']): r for r in rows}


def get(family, pattern, buckets, codec, operation, backend='simd', n=120, timing='regular', suite='BenchmarkALPMatrix'):
    return index[backend, suite, family, pattern, n, buckets, timing, codec, operation]


def pair(family, pattern, buckets, operation, n=120):
    legacy, alp = ('XOR2', 'ALP') if family == 'float' else ('legacy', 'ALPv3')
    return get(family, pattern, buckets, legacy, operation, n=n), get(family, pattern, buckets, alp, operation, n=n)


def table(headers, body):
    return '\n'.join(['| ' + ' | '.join(headers) + ' |', '| ' + ' | '.join(['---'] * len(headers)) + ' |'] + ['| ' + ' | '.join(row) + ' |' for row in body])


highlights = [
    ('Two-digit decimals', 'float', 'decimal2', 0),
    ('Arbitrary float bits', 'float', 'random-bits', 0),
    ('Smooth integer histograms', 'integer-histogram', 'smooth', 8),
    ('Bursty integer histograms', 'integer-histogram', 'bursty', 8),
    ('Fractional float histograms', 'float-histogram', 'fractional', 128),
    ('Noisy fractional histograms', 'float-histogram', 'noisy-fractional', 8),
]
text = '''# Extensive ALP compression, decompression, and CPU benchmark

Measured September 29, 2026, on Apple M5 Pro, darwin/arm64, Go 1.27.1.
Implementation and benchmark revision: `660ba97d7e068bd969f973c19a8f9970cc203efa`.

**ALP is substantially better at warm full-batch decoding in this matrix, but
usually costs more CPU to compress. It does not consistently make histograms
smaller.** Adaptive selection remains important.

There are **4,212 observations**: 79 scenarios, 351 operations, two builds,
and six repetitions. The builds use identical source and compiler versions;
only the SIMD experiment differs. Both run with `GOMAXPROCS=1`, `GOGC=100`.
The timed matrix took about 12 minutes 47 seconds.

- SIMD ALP warm native decoding was faster in **72/72** full-codec scenarios,
  with observed speedups from **1.19× to 7.74×**.
- Full compression used more CPU in **66/72** scenarios; six favored ALP.
- ALP stored fewer bytes in **49/72** scenarios. This is a case count, not a
  production-workload compression estimate.
- Median CPU utilization across benchmark rows was about **99.8% of one core**.
  Both codecs keep a core busy. The meaningful resource difference is CPU time
  required to process the same number of observations.

These are deterministic synthetic, memory-resident codec benchmarks. They do
not measure end-to-end ingestion, disk I/O, or PromQL query speed. Warm decoding
materializes every value/histogram; it is not a decode-kernel-only measurement.

## Explore the complete results

- [Interactive comparison](explorer.html): filter type, size, SIMD/scalar,
  timestamp pattern, decode mode, throughput, and CPU cost.
- [All medians in CSV](summary.csv), [machine-readable medians](summary.json),
  and [all comparison tables](tables.md).
- [Detailed methodology and reproduction](methodology.md).
- Six-run benchstat: [SIMD ALP versus legacy](simd-codecs-benchstat.txt),
  [scalar ALP versus legacy](scalar-codecs-benchstat.txt), and
  [scalar versus SIMD](scalar-vs-simd-benchstat.txt).
- [Raw observations](results.json), [per-process diagnostics](processes.json),
  [binary hashes and execution metadata](metadata.json), and individual logs in
  [raw/](raw/).

## Representative full-compression and decompression results

All rows below contain 120 input observations. Times are microseconds per whole
input batch. Histograms use the indicated total bucket count. Legacy means XOR2
for floats and the existing ST histogram codec for histograms. ALP histogram
results here are version 3. A positive size change means ALP stores more bytes.

'''
body = []
for label, family, pattern, buckets in highlights:
    e0, e1 = pair(family, pattern, buckets, 'encode')
    d0, d1 = pair(family, pattern, buckets, 'decode')
    body.append([label, str(buckets), f"{e0['ns/op']/1000:.3f} → {e1['ns/op']/1000:.3f}", f"{d0['ns/op']/1000:.3f} → {d1['ns/op']/1000:.3f}", f"{e1['cpu-ns/sample']/e0['cpu-ns/sample']:.2f}×", f"{d1['cpu-ns/sample']/d0['cpu-ns/sample']:.2f}×", f"{(e1['encoded-B/sample']/e0['encoded-B/sample']-1)*100:+.1f}%"])
text += table(['Input', 'Buckets', 'Compress µs legacy → ALP', 'Decompress µs legacy → ALP', 'Compression CPU ALP/legacy', 'Decompression CPU ALP/legacy', 'Encoded size change'], body)
text += '''

For two-digit decimals, this corresponds to roughly **62.8 → 41.6 million
samples/s for compression**, and **127.8 → 304.4 million samples/s for
decompression**. ALP uses about half again as much compression CPU, but around
58% less decompression CPU and 76% fewer stored bytes on that fixture.

For 128 fractional histogram buckets, decompression is about **6.04× faster**,
with about **83.5% less CPU** and **76.1% fewer bytes**. Compression costs about
**1.80×** the CPU. The noisy eight-bucket fractional case is less attractive:
roughly **3×** compression CPU for only **7.1%** space savings.

## CPU usage in absolute units

These are **CPU seconds per million input observations**, measured with process
user/system CPU counters around the benchmark loop. One histogram observation
means the entire histogram, not a bucket. Percent utilization alone would hide
these differences because both implementations normally occupy one core.

'''
body = []
for label, family, pattern, buckets in highlights:
    e0, e1 = pair(family, pattern, buckets, 'encode'); d0, d1 = pair(family, pattern, buckets, 'decode')
    body.append([label, f"{e0['cpu-seconds/M-samples']:.4f}", f"{e1['cpu-seconds/M-samples']:.4f}", f"{d0['cpu-seconds/M-samples']:.4f}", f"{d1['cpu-seconds/M-samples']:.4f}"])
text += table(['Input', 'Legacy compression CPU s/M', 'ALP compression CPU s/M', 'Legacy decompression CPU s/M', 'ALP decompression CPU s/M'], body)
text += '''

Per-row median utilization ranged from about **95% to 100% of one core**.
CSV/raw data retain user CPU, system CPU, total CPU, and core utilization
separately. Process startup and fixture generation are excluded from these
per-observation costs. The outer subprocess CPU totals are retained separately
and are not used to infer compression/decompression CPU costs.

## Where forced ALP loses

The complete matrix contains 23 size regressions. Important examples include:

'''
losses = [
    ('Constant floats, 32 samples', 'float', 'constant', 0, 32),
    ('Constant floats, 120 samples', 'float', 'constant', 0, 120),
    ('Bursty integer histograms', 'integer-histogram', 'bursty', 8, 120),
    ('Resetting integer histograms', 'integer-histogram', 'resets', 8, 120),
    ('Stale-heavy integer histograms', 'integer-histogram', 'stale', 8, 120),
    ('Smooth float histograms', 'float-histogram', 'smooth', 128, 120),
]
body = []
for label, family, pattern, buckets, n in losses:
    a, b = pair(family, pattern, buckets, 'encode', n)
    body.append([label, str(buckets), f"{a['encoded-B/sample']:.3f}", f"{b['encoded-B/sample']:.3f}", f"{(b['encoded-B/sample']/a['encoded-B/sample']-1)*100:.1f}%", f"{b['cpu-ns/sample']/a['cpu-ns/sample']:.2f}×"])
text += table(['Input', 'Buckets', 'Legacy B/sample', 'ALP B/sample', 'Size increase', 'Compression CPU ALP/legacy'], body)
text += '''

ALP also spends about **5.45×** the compression CPU on arbitrary float bits at
120 samples, while saving only **2.2%** of the bytes. That fails the adaptive
5% size criterion. Computed sine values save only **4.7%** at 120 samples while
requiring roughly **4.69×** the compression CPU. Their faster decoding does not
make unconditional conversion an obvious choice.

All sizes include timestamps, start timestamps, headers, layouts, hints, and
all chunks created by resets. These losses are not artifacts of excluding
metadata or counting only the last chunk.

## Chunk size and wider histograms

These controlled tests force the indicated number of samples into a batch.
The 1,024-sample float cases are scaling stress tests, not the normal TSDB
byte-based chunk-cutting policy. The full matrix covers every listed float
pattern at 32, 120, and 1,024 samples.

'''
body = []
for n in [32, 120, 1024]:
    e0,e1=pair('float','decimal2',0,'encode',n);d0,d1=pair('float','decimal2',0,'decode',n)
    body.append([str(n), f"{e0['encoded-B/sample']:.3f} → {e1['encoded-B/sample']:.3f}", f"{e0['M-samples/s']:.2f} → {e1['M-samples/s']:.2f}", f"{d0['M-samples/s']:.2f} → {d1['M-samples/s']:.2f}"])
text += table(['Decimal samples/batch', 'B/sample legacy → ALP', 'Compress M samples/s legacy → ALP', 'Decompress M samples/s legacy → ALP'], body)
text += '\n\nInteger-to-float materialization, which matters for query consumers, is measured separately:\n\n'
body=[]
for buckets in [8,128,1031]:
    a,b=pair('integer-histogram','smooth',buckets,'decode-float')
    body.append([str(buckets),f"{a['ns/op']/1000:.3f}",f"{b['ns/op']/1000:.3f}",f"{a['ns/op']/b['ns/op']:.2f}×",f"{b['cpu-ns/sample']/a['cpu-ns/sample']:.2f}×"])
text += table(['Buckets', 'Legacy µs/batch', 'ALP µs/batch', 'Throughput speedup', 'ALP/legacy CPU'],body)
text += '''

All 17 integer-to-float cases improved in this run, with speedups between
**1.29× and 6.37×**. A whole histogram is still one observation in these rates.

## SIMD contribution

Comparing ALP with legacy conflates format, predictor, timestamp, and iterator
changes with SIMD. The following table isolates SIMD by comparing ALP's scalar
and NEON builds using the same Go version and fixtures. Values above one mean
the SIMD build is faster. They are observed ratios; use the benchstat data and
unchanged-codec controls when interpreting small differences.

'''
body=[]
for label,family,pattern,buckets in [highlights[0], highlights[1], ('Smooth integer histograms','integer-histogram','smooth',128), highlights[4], highlights[5]]:
    codec='ALP' if family=='float' else 'ALPv3'
    e0=get(family,pattern,buckets,codec,'encode','scalar');e1=get(family,pattern,buckets,codec,'encode')
    d0=get(family,pattern,buckets,codec,'decode','scalar');d1=get(family,pattern,buckets,codec,'decode')
    body.append([label,str(buckets),f"{e0['ns/op']/e1['ns/op']:.3f}×",f"{d0['ns/op']/d1['ns/op']:.3f}×",f"{d1['cpu-ns/sample']/d0['cpu-ns/sample']:.3f}×"])
text += table(['Input','Buckets','Compression SIMD speedup','Decompression SIMD speedup','SIMD/scalar decompression CPU'],body)
text += '''

NEON is useful, but it is not the sole explanation for ALP's decoding advantage.
For example, two-digit decimal ALP decoding gains about 6% from SIMD while
ALP itself decodes about 2.38× faster than XOR2. Arbitrary-bit/raw data and
exception-heavy paths do not have the same SIMD opportunity as decimal blocks.
These results do not predict AVX2 or AVX-512 performance.

## Cold allocation and histogram version tradeoffs

Cold decoding allocates fresh iterator/result buffers and reads the entire
batch; the serialized bytes remain in RAM. Three of four primary comparisons
favored ALP; one did not. These are not first-sample-latency or cold-disk tests.

'''
body=[]
for family,pattern,buckets in [('float','decimal2',0),('float','random-bits',0),('integer-histogram','smooth',8),('float-histogram','smooth',8)]:
    a,b=pair(family,pattern,buckets,'decode-cold')
    body.append([family+' / '+pattern,f"{a['ns/op']/1000:.3f} → {b['ns/op']/1000:.3f}",f"{a['B/op']:.0f} → {b['B/op']:.0f}",f"{a['allocs/op']:.0f} → {b['allocs/op']:.0f}",f"{b['cpu-ns/sample']/a['cpu-ns/sample']:.2f}×"])
text += table(['Input','µs/batch legacy → ALP','Allocated B/batch legacy → ALP','Allocations/batch legacy → ALP','ALP/legacy CPU'],body)
text += '\n\nFor the eight-bucket smooth reference fixture, version 3 improves size relative to version 1 but does not necessarily minimize decode time:\n\n'
body=[]
for family in ['integer-histogram','float-histogram']:
    for codec in ['legacy','ALPv1','ALPv3']:
        e=get(family,'smooth',8,codec,'encode');d=get(family,'smooth',8,codec,'decode')
        body.append([family,codec,f"{e['encoded-B/sample']:.3f}",f"{e['ns/op']/1000:.3f}",f"{d['ns/op']/1000:.3f}"])
text += table(['Input','Codec','B/sample','Compress µs/batch','Decompress µs/batch'],body)
text += '''

Version 1 remains the explicit histogram `alp` writer; adaptive compaction
trials version 3. The versions must not be silently mixed when comparing results.

## Incremental compaction and adaptive selection

The source is already encoded in these measurements. This is extra conversion
cost, not full compression throughput. One encoder is reused for a repeated,
homogeneous series. Float rejection hints can bypass seven of eight trials.

'''
body=[]
for family,pattern,buckets in [('float','decimal2',0),('float','computed',0),('float','random-bits',0),('integer-histogram','smooth',8),('integer-histogram','bursty',128),('float-histogram','fractional',8),('float-histogram','noisy-fractional',128)]:
    a=get(family,pattern,buckets,'forced','transcode',suite='BenchmarkALPMatrixTranscode');b=get(family,pattern,buckets,'adaptive','transcode',suite='BenchmarkALPMatrixTranscode')
    body.append([family+' / '+pattern,str(buckets),f"{a['ns/op']/1000:.3f} → {b['ns/op']/1000:.3f}",f"{a['cpu-seconds/M-samples']:.5f} → {b['cpu-seconds/M-samples']:.5f}",f"{b['accepted-%']:.0f}%"])
text += table(['Input','Buckets','µs/batch forced → adaptive','CPU s/M forced → adaptive','Adaptive accepted'],body)
text += '''

Adaptive float selection removes most wasted conversion CPU for the tested
computed and arbitrary-bit series. Histogram selection still pays for a full
candidate before rejecting it: the bursty 128-bucket histogram retains legacy
bytes but consumes almost the same conversion CPU as forced ALP.

Warm parameter hints can select different decimal plans than fresh full
compression. Consequently, the incremental conversion fixture can have a
different encoded size from the cold encoder even for identical logical data.
The raw results report the actual retained size and acceptance percentage.

## A rough CPU-only break-even estimate

Dividing additional compression CPU by CPU saved per complete decode gives an
illustrative number of full read passes needed to recover the extra encoding
CPU. This ignores I/O, caching, selective queries, concurrency, and memory costs.
It is not a prediction of how many production queries make ALP worthwhile.

'''
body=[]
for label,family,pattern,buckets in highlights:
    e0,e1=pair(family,pattern,buckets,'encode');d0,d1=pair(family,pattern,buckets,'decode')
    passes=(e1['cpu-ns/sample']-e0['cpu-ns/sample'])/(d0['cpu-ns/sample']-d1['cpu-ns/sample'])
    body.append([label,str(buckets),f'{passes:.1f}'])
text += table(['Input','Buckets','Approximate complete read passes'],body)
text += '''

## Interpretation and limits

The strongest cases here are decimal float data and read-heavy fractional
histograms. Full encoding is usually more expensive; the six cases where ALP
compresses faster do not justify generalizing that result to other patterns.
Warm decoding is consistently favorable, but cold allocation, small chunks,
and encoded size need separate evaluation.

Keep the size gate for histograms: bursty, resetting, and stale-heavy integer
fixtures demonstrate real size regressions. A cheaper histogram rejection test
would be valuable because the present size gate avoids larger output but does
not avoid the CPU cost of constructing a rejected candidate.

The numerical results are fixture-specific. The scalar/SIMD byte sizes and
chunk counts match, all 702 reported medians contain six observations, and
input/output correctness tests passed in both builds. Go lint passed for the
harness. Statistical comparisons include all raw repetitions and unchanged
legacy controls; no end-to-end query or production-workload claim is made.

To regenerate the summaries and this report from the saved measurements:

```sh
python3 tsdb/docs/benchmarks/alp-extensive-20260929/analyze.py \\
  tsdb/docs/benchmarks/alp-extensive-20260929
python3 tsdb/docs/benchmarks/alp-extensive-20260929/report.py
```
'''
(root/'report.md').write_text(text)
print('Wrote',root/'report.md')
