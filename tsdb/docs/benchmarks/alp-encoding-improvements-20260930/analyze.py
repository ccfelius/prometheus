"""Summarize paired encoding benchmarks and generate the implementation report."""
from collections import defaultdict
import csv
import json
from pathlib import Path
import re
import statistics

root=Path(__file__).resolve().parent
summary=[]
for build in ('before','after'):
    groups=defaultdict(list)
    for line in (root/f'paired-{build}.txt').read_text().splitlines():
        if not line.startswith('Benchmark'): continue
        tokens=line.split()
        name=re.sub(r'-\d+$','',tokens[0])
        groups[name].append({tokens[i+1]:float(tokens[i]) for i in range(2,len(tokens),2)})
    for name,observations in groups.items():
        assert len(observations)==6,(build,name,len(observations))
        row={'build':build,'benchmark':name,'observations':len(observations)}
        row.update({metric:statistics.median(o[metric] for o in observations) for metric in observations[0]})
        row['min-ns/op']=min(o['ns/op'] for o in observations)
        row['max-ns/op']=max(o['ns/op'] for o in observations)
        summary.append(row)
(root/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
columns=['build','benchmark','observations','ns/op','cpu-ns/sample','encoded-B/sample','B/op','allocs/op','cpu-%core','accepted-%','min-ns/op','max-ns/op']
with (root/'summary.csv').open('w',newline='') as f:
    writer=csv.DictWriter(f,columns,extrasaction='ignore',lineterminator='\n')
    writer.writeheader();writer.writerows(summary)
index={(r['build'],r['benchmark']):r for r in summary}
for codec in ('ALPv3', 'ALPv4'):
    lines=[]
    for line in (root/'paired-after.txt').read_text().splitlines():
        if line.startswith(('goos:', 'goarch:', 'pkg:', 'cpu:')):
            lines.append(line)
        elif line.startswith('BenchmarkALPMatrix/') and f'/{codec}/' in line:
            lines.append(line.replace(f'/{codec}/', '/ALP/'))
    (root/f'{codec}.txt').write_text('\n'.join(lines)+'\n')

def get(build,family,pattern,buckets,codec,op='encode',timing='regular'):
    return index[build,f'BenchmarkALPMatrix/{family}/{pattern}/n=120/buckets={buckets}/time={timing}/{codec}/{op}']

def table(headers,rows):
    return '\n'.join(['| '+' | '.join(headers)+' |','| '+' | '.join(['---']*len(headers))+' |']+['| '+' | '.join(r)+' |' for r in rows])

meta=json.loads((root/'metadata.json').read_text())
text=f'''# ALP encoding improvements

The default encoding improvements reduce wasted selection work, allocations,
and scalar packing. Histogram v4 is implemented as an explicit experimental
conversion API because smaller output can require additional encoding work.
Configured histogram writers now consistently emit v3; v1 and v2 remain readable.

Measurements use identical Go 1.27.1 SIMD toolchains on Apple M5 Pro, ARM64.
The final comparison alternates frozen before/after binaries across 29 shuffled
scenarios, with six repetitions per operation. It contains {len(summary)*6:,}
observations and {len(summary)} medians. Final measurement began
`{meta['started_utc']}`. Earlier exploratory runs began September 30 and are
preserved separately. No AMD64 runtime performance is claimed.
The baseline revision is `a0de5333b`; the measured final source is
`{meta['revision']}`. Binary hashes and embedded build information are recorded
in the metadata. The final binary was built immediately before committing that
source, so its embedded VCS revision names the preceding commit plus local edits.

Every paired existing-format case retained the same encoded bytes per sample.
This is fixture coverage, not a guarantee that bounded parameter search always
selects the same representation on other data.

## Float encoding

120 samples, regular timestamps, no start timestamps. CPU cost is process
user plus system nanoseconds per sample, numerically equivalent to milliseconds
per million samples. Values are medians. The baseline is the previous ALP
implementation, not XOR2; unchanged XOR2 controls are in the full data.

'''
rows=[]
for pattern in ('constant','decimal2','decimal6','computed','random-bits'):
    a=get('before','float',pattern,0,'ALP',timing='no-st');b=get('after','float',pattern,0,'ALP',timing='no-st')
    rows.append([pattern,f"{a['ns/op']/1000:.3f} → {b['ns/op']/1000:.3f}",f"{a['ns/op']/b['ns/op']:.2f}×",f"{a['cpu-ns/sample']:.2f} → {b['cpu-ns/sample']:.2f}",f"{int(a['B/op']):,} → {int(b['B/op']):,}",f"{int(a['allocs/op'])} → {int(b['allocs/op'])}",f"{a['encoded-B/sample']:.3f} → {b['encoded-B/sample']:.3f}"])
text+=table(['Pattern','Encode µs/chunk before → after','Speedup','CPU ns/sample','Allocated B/chunk','Allocations/chunk','Encoded B/sample'],rows)
text+='''

## Decoding existing formats

These timings include materializing all 120 samples and reuse the iterator and
histogram destination. The table compares the previous and final ALP code for
the same format. Float cases omit start timestamps; histogram cases include them.

'''
rows=[]
for family,pattern,buckets,codec,timing in [
    ('float','decimal2',0,'ALP','no-st'),
    ('float','computed',0,'ALP','no-st'),
    ('float','random-bits',0,'ALP','no-st'),
    ('integer-histogram','smooth',8,'ALPv3','regular'),
    ('integer-histogram','smooth',128,'ALPv3','regular'),
    ('integer-histogram','bursty',128,'ALPv3','regular'),
    ('float-histogram','smooth',128,'ALPv3','regular'),
    ('float-histogram','noisy-fractional',128,'ALPv3','regular'),
]:
    a=get('before',family,pattern,buckets,codec,'decode',timing)
    b=get('after',family,pattern,buckets,codec,'decode',timing)
    rows.append([family+'/'+pattern,str(buckets),f"{a['ns/op']/1000:.3f} → {b['ns/op']/1000:.3f}",f"{a['cpu-ns/sample']:.2f} → {b['cpu-ns/sample']:.2f}",f"{a['ns/op']/b['ns/op']:.2f}×"])
text+=table(['Input','Buckets','Decode µs/chunk','CPU ns/sample','Speedup'],rows)
text+='''

## Incremental histogram conversion

The source is already encoded. A reusable encoder processes repeated chunks
from one homogeneous series. Adaptive rejection results therefore include
amortized retry/backoff costs; they are not the latency of a cold first decision.
One observation is a whole histogram. Forced conversion in this table uses v3.
For forced conversion, 100% accepted only means a candidate was returned; it
does not mean it was smaller. Adaptive conversion enforces the savings policy.

'''
rows=[]
for family,pattern,buckets in [('integer-histogram','smooth',8),('integer-histogram','bursty',128),('float-histogram','fractional',8),('float-histogram','noisy-fractional',128)]:
    for policy in ('forced','adaptive'):
        name=f'BenchmarkALPMatrixTranscode/{family}/{pattern}/n=120/buckets={buckets}/time=regular/{policy}'
        a,b=index['before',name],index['after',name]
        rows.append([family+'/'+pattern,str(buckets),policy,f"{a['ns/op']/1000:.2f} → {b['ns/op']/1000:.2f}",f"{a['ns/op']/b['ns/op']:.2f}×",f"{a['cpu-ns/sample']:.1f} → {b['cpu-ns/sample']:.1f}",f"{int(a['B/op']):,} → {int(b['B/op']):,}",f"{int(a['allocs/op'])} → {int(b['allocs/op'])}",f"{b['accepted-%']:.1f}%"])
text+=table(['Input','Buckets','Policy','Convert µs/chunk','Speedup','CPU ns/snapshot','Allocated B/chunk','Allocations','Accepted after'],rows)
text+='''

## Histogram v4 tradeoffs

These results compare v3 and v4 from the same final binary, including appending
through the existing mutable histogram codec and final serialization. Decode
materializes every histogram with reused output buffers. The existing codec is
the ST histogram encoding. All rows have 120 snapshots per input batch.

'''
rows=[]
for family,pattern,buckets in [('integer-histogram','smooth',8),('integer-histogram','bursty',8),('integer-histogram','bursty',128),('float-histogram','smooth',8),('float-histogram','smooth',128),('float-histogram','fractional',128),('float-histogram','noisy-fractional',128)]:
    old=get('after',family,pattern,buckets,'legacy')
    a=get('after',family,pattern,buckets,'ALPv3');b=get('after',family,pattern,buckets,'ALPv4')
    da=get('after',family,pattern,buckets,'ALPv3','decode');db=get('after',family,pattern,buckets,'ALPv4','decode')
    rows.append([family+'/'+pattern,str(buckets),f"{old['encoded-B/sample']:.2f}",f"{a['encoded-B/sample']:.2f} → {b['encoded-B/sample']:.2f}",f"{a['ns/op']/1000:.2f} → {b['ns/op']/1000:.2f}",f"{da['ns/op']/1000:.2f} → {db['ns/op']/1000:.2f}"])
text+=table(['Input','Buckets','Existing B/snapshot','ALP B/snapshot v3 → v4','Encode µs v3 → v4','Decode µs v3 → v4'],rows)
text+='''

V4 is not automatically selected by `histograms: auto`. Its extra predictor and
exception searches cost CPU, and it is not universally smaller than the existing
histogram codec. The explicit `ALPEncoder.RecodeHistogramV4` API makes the format
available for controlled evaluation. Existing v3 output remains the default.

## What changed

1. **Histogram adaptive rejection.** `RecodeHistogramIfSmaller` owns the size
   policy used by compaction. It stops trials once committed numeric/time bytes
   exceed the allowed complete size, uses a conservative 32-snapshot prefix
   estimate when enough complete blocks exist, and retries rejected layouts
   every eighth chunk. The prefix ignores the initial predictor transient and
   requires a 25% projected overshoot. Layout/reset/sample-count changes and
   series boundaries invalidate rejection hints. Final acceptance still requires
   at least 5% savings. Sampling can miss compression opportunities.
2. **Float selection.** The initial adaptive probe returns its decimal plan to
   the full encoder. Full validation remains mandatory. A failed common-scale
   sample no longer expands to all 190 parameter pairs when none appears better
   than raw storage. RD/raw fallback remains exact. Already evaluated finalist
   pairs are skipped. Less search can change the chosen encoding or miss savings
   on workloads outside the measured fixtures.
3. **Allocation and copying.** Fresh float chunks lazily reserve one 128-sample
   pending block. Histogram conversion reserves bounded numeric capacity from
   the source size, then allocates the final owned output exactly once. Numeric
   reservation is capped at 256 KiB; retained scratch still obeys the existing
   64 KiB per-buffer limit. Returned bytes never alias reusable scratch. The
   float reservation improves common chunks but reserves more memory for very
   short active series than the old geometric-growth approach.
4. **SIMD encoding.** Packing now operates across independent lanes on NEON,
   AVX2, and AVX-512 while preserving the existing vertical byte layout. Partial
   vectors use bounded scalar tails. NEON and AVX-512 combine decimal conversion,
   min/max, and exception counting; AVX2 retains its full-range fallback and
   separate reduction. Integer reconstruction for v4 float counts is also
   vectorized, with a scalar path for strides narrower than the hardware vector.
5. **Consistent explicit writing.** New explicit ALP histogram chunks and forced
   conversion now emit v3. Existing persisted chunks retain their format on
   append resumption. Readers for v1/v2 remain available; readers predating v3
   cannot read the new configured output.
6. **Experimental v4.** Integer blocks can patch sparse large residuals and
   select delta or delta-of-delta. Bucket prediction operates on signed delta
   bits before zigzag. Float blocks can encode temporal differences after exact
   decimal conversion; initial values and subsequent differences use separate
   integer frames. Special values fall back to ordinary lossless ALP. Every
   alternative is compared with its local baseline before being selected.

An initial paired run exposed a 7–14% regression in v3 integer-histogram
decoding after adding v4 format dispatch. Hoisting the format checks outside
the per-bucket materialization loops removed that regression in a focused
rerun. The final paired results include that fix. The diagnostic run and its
statistics are preserved under [pre-hoist/](pre-hoist/).

The final run still records small statistically significant regressions: v3
integer-histogram decoding costs about 1–3.4% more, and wide smooth integer
encoding costs 2.2% more. The case with the largest percentage increase adds about
1.1 ns per snapshot (4.027 to 4.163 microseconds per 120-snapshot chunk).
These costs remain visible and are retained as the tradeoff for shared v4
support; they are not reported as improvements or dismissed as noise. Further
dispatch specialization is a possible follow-up. V4's much larger temporal
float decoding cost is an additional reason it remains explicitly opt-in.

The [format specification](../../format/alp_histograms.md) documents the complete
v4 representation, arithmetic, validation, and compatibility rules.

## Deliberately retained design choices

The mutable ST histogram appender remains responsible for layout recoding,
counter resets, staleness, and snapshots. Replacing it with a raw buffered
builder would require preserving those semantics while bounding active-series
memory. For example, 120 snapshots with 128 buckets contain 125,760 raw numeric
bytes before timestamps or layout, versus about 21,264 encoded bytes for the
smooth float fixture's existing codec. This comparison is not a memory profile,
but it illustrates why retaining all raw snapshots is not an acceptable default.
A bounded direct builder and broader semantic field grouping remain follow-up
work; this change does not claim to remove the first legacy encoding stage.

## Validation and limits

- Full scalar and ARM64 SIMD chunkenc tests passed, including all matrix fixtures.
- ALP compaction and remote-read integration tests passed.
- Race-enabled codec, TSDB, and remote-read tests passed.
- Histogram decoder fuzzing completed 705,027 executions without a failure.
- All packing widths, partial vectors, scalar byte parity, signed/unsigned
  extremes, stale NaNs, signed zero, custom layouts, and reset behavior are covered.
- New malformed-input tests cover exception ordering/ranges, frame overflow,
  temporal stride, lengths, exponent/factor bounds, and truncation.
- Linux AMD64 SIMD compilation passed. AVX2/AVX-512 runtime testing is outstanding.
- `make lint` passed with the SIMD experiment enabled.

Captured check output is in [validation/](validation/). Race, fuzz, integration,
and cross-compilation checks ran before the final loop-hoisting change; full
scalar/SIMD codec tests and lint were rerun afterward.

These are synthetic codec benchmarks on one ARM64 machine, with
`GOMAXPROCS=1`, `GOGC=100`, 150 ms per repetition and six repetitions. They do not
measure end-to-end ingestion, PromQL, disk I/O, or production workload mixes.
CPU percentage is process CPU time divided by wall time, expressed as a
percentage of one core. These serial CPU-bound loops usually saturate one core;
CPU nanoseconds per sample is the useful comparison of work consumed. Histogram
CPU costs are per complete snapshot, not per bucket. Allocation counts and bytes
are per whole chunk operation. This is not an RSS or peak-memory measurement.
The unchanged XOR2 and legacy histogram controls help assess host timing drift;
small differences should be read with the statistical comparisons.

This incremental report reruns XOR2 and existing histogram controls. The broader
[XOR/XOR2/ALP comparison](../alp-xor-xor2-20260930/report.md) includes original XOR;
original XOR was not rerun in this change's paired experiment.

[All medians](summary.csv), [machine-readable medians](summary.json),
[paired benchstat](benchstat.txt), [v3 versus v4 benchstat](v3-v4-benchstat.txt),
and [execution metadata](metadata.json) are
included. Individual process output is in [raw/](raw/). `before.txt`, `stage1.txt`,
and `after.txt` preserve the earlier exploratory runs; final conclusions use
`paired-before.txt` and `paired-after.txt` exclusively.

## Reproduction

Compile the baseline revision `a0de5333b` in a separate checkout to
`/tmp/alp-improve-before.test`, and the final implementation to
`/tmp/alp-improve-final.test`, both with:

```sh
GOTOOLCHAIN=go1.27.1 GOEXPERIMENT=simd go test -c ./tsdb/chunkenc -o /tmp/SELECTED-BINARY.test
```

From the final checkout:

```sh
python3 tsdb/docs/benchmarks/alp-encoding-improvements-20260930/run.py --before /tmp/alp-improve-before.test --after /tmp/alp-improve-final.test
python3 tsdb/docs/benchmarks/alp-encoding-improvements-20260930/analyze.py
cd tsdb/docs/benchmarks/alp-encoding-improvements-20260930
GOTOOLCHAIN=go1.27.1 go run golang.org/x/perf/cmd/benchstat@v0.0.0-20260908200009-22c9c6c9d4da paired-before.txt paired-after.txt > benchstat.txt
GOTOOLCHAIN=go1.27.1 go run golang.org/x/perf/cmd/benchstat@v0.0.0-20260908200009-22c9c6c9d4da ALPv3.txt ALPv4.txt > v3-v4-benchstat.txt
```

The runner records binary hashes and build metadata, serializes timed processes,
alternates before/after order by case, and validates six observations per row.
'''
(root/'report.md').write_text(text)
print(f'{len(summary)} medians; report generated.')
