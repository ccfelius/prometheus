"""Summarize the complete current ALP matrix without rerunning measurements."""
import csv
from datetime import datetime
import importlib.util
import json
from pathlib import Path
import statistics
import sys

sys.dont_write_bytecode = True
root = Path(__file__).resolve().parent
source = root.parent / 'alp-extensive-20260929' / 'analyze.py'
spec = importlib.util.spec_from_file_location('shared_analysis', source)
shared = importlib.util.module_from_spec(spec)
spec.loader.exec_module(shared)
meta = json.loads((root / 'metadata.json').read_text())
assert 'completed_utc' in meta, 'Wait for the complete benchmark before reporting.'
profile_meta = json.loads((root / 'profiles' / 'metadata.json').read_text())
profile_date = profile_meta['completed_utc'][:10]
kevent_share = next((line.split()[1] for line in (root / 'profiles' / 'float-decimal2-XOR2-encode-flat.txt').read_text().splitlines() if line.split() and line.split()[-1] == 'runtime.kevent'), 'not listed')
processes = json.loads((root / 'processes.json').read_text())
process_seconds = sum(p['wall_seconds'] for p in processes)
utc_seconds = (datetime.fromisoformat(meta['completed_utc']) - datetime.fromisoformat(meta['started_utc'])).total_seconds()
rows = shared.summarize(root)
assert len(rows) == 1288, len(rows)
assert all(r['observations'] == 6 for r in rows)
assert meta['observations'] == 7728
keys = ['backend', 'suite', 'family', 'pattern', 'samples', 'buckets', 'timing', 'codec', 'operation']
index = {tuple(r[k] for k in keys): r for r in rows}


def get(family, pattern, n, buckets, timing, codec, op='encode', backend='simd', suite='BenchmarkALPMatrix'):
    return index[backend, suite, family, pattern, n, buckets, timing, codec, op]


def numeric_size(row):
    if row['family'] == 'float':
        return 8
    n, buckets, pattern = row['samples'], row['buckets'], row['pattern']
    return sum(8 * (3 + (0 if pattern == 'stale' and i % 20 == 19 else buckets + (2 if pattern == 'layout' and i >= n // 2 else 0))) for i in range(n)) / n


for row in rows:
    row['numeric-B/sample'] = numeric_size(row)
    row['numeric-compression-ratio'] = row['numeric-B/sample'] / row['encoded-B/sample']
    other_key = tuple('scalar' if k == 'backend' else row[k] for k in keys)
    assert row['encoded-B/sample'] == index[other_key]['encoded-B/sample'], row['benchmark']
(root / 'summary.json').write_text(json.dumps(rows, indent=2) + '\n')
columns = list(rows[0])
for row in rows:
    columns.extend(k for k in row if k not in columns)
with (root / 'summary.csv').open('w', newline='') as stream:
    writer = csv.DictWriter(stream, columns, lineterminator='\n')
    writer.writeheader()
    writer.writerows(rows)

comparisons = []
for row in rows:
    if row['suite'] != 'BenchmarkALPMatrix' or row['codec'] not in ('ALP', 'ALPv3', 'ALPv4'):
        continue
    baselines = ('XOR', 'XOR2') if row['family'] == 'float' else ('legacy',)
    for baseline in baselines:
        key = tuple(baseline if k == 'codec' else row[k] for k in keys)
        if key not in index:
            continue
        ref = index[key]
        entry = {k: row[k] for k in keys}
        entry.update(baseline=baseline, speedup=ref['ns/op'] / row['ns/op'],
                     cpu_ratio=row['cpu-ns/sample'] / ref['cpu-ns/sample'],
                     size_ratio=row['encoded-B/sample'] / ref['encoded-B/sample'])
        comparisons.append(entry)
(root / 'comparisons.json').write_text(json.dumps(comparisons, indent=2) + '\n')
with (root / 'comparisons.csv').open('w', newline='') as stream:
    writer = csv.DictWriter(stream, list(comparisons[0]), lineterminator='\n')
    writer.writeheader()
    writer.writerows(comparisons)

# Identical normalized names allow benchstat to compare each codec directly.
for backend in ('scalar', 'simd'):
    raw = (root / f'{backend}.txt').read_text().splitlines()
    headers = [line for line in raw if line.startswith(('goos:', 'goarch:', 'pkg:', 'cpu:'))]
    for codec in ('XOR', 'XOR2', 'ALP', 'legacy', 'ALPv3', 'ALPv4'):
        selected = [line.replace(f'/{codec}/', '/codec/') for line in raw if line.startswith('BenchmarkALPMatrix/') and f'/{codec}/' in line]
        (root / f'{backend}-{codec.lower()}.txt').write_text('\n'.join(headers + selected) + '\n')


def table(headers, body):
    return '\n'.join(['| ' + ' | '.join(headers) + ' |', '| ' + ' | '.join(['---'] * len(headers)) + ' |'] + ['| ' + ' | '.join(row) + ' |' for row in body])


def fmt(value, digits=2):
    return f'{value:,.{digits}f}'


confirmation_meta = json.loads((root / 'confirmation' / 'metadata.json').read_text())
assert 'completed_utc' in confirmation_meta
confirmation_rows = shared.summarize(root / 'confirmation')
confirmation_index = {(r['backend'], r['benchmark']):r for r in confirmation_rows}
primary_index = {(r['backend'], r['benchmark']):r for r in rows}
confirmation_text = '''# Confirmation of variable benchmark rows

The primary matrix retains every measurement. We selected every primary row
whose largest ns/op exceeded 1.5 times its smallest, then repeated all codecs
and both builds for the affected case and operation. Confirmations use six
500 ms repetitions, twice the primary repetition duration. The rule was selected
while the primary run was in progress; it is a variability check, not an
independent preregistered experiment or a license to discard inconvenient data.

The table below lists flagged rows. The complete matched controls and repeats
are in [summary.csv](summary.csv) and [raw/](raw/). Primary and confirmation
measurements are kept separate and are never averaged together.

'''
body = []
for flagged in confirmation_meta['flagged']:
    key = flagged['backend'], flagged['benchmark']
    primary, confirmation = primary_index[key], confirmation_index[key]
    body.append([flagged['backend'], flagged['benchmark'].removeprefix('BenchmarkALPMatrix/'),
                 fmt(flagged['max_min_ratio']), fmt(primary['ns/op']/1000, 3),
                 fmt(confirmation['ns/op']/1000, 3),
                 fmt(confirmation['max-ns/op']/confirmation['min-ns/op'])])
confirmation_text += table(['Build', 'Benchmark', 'Primary max/min', 'Primary median µs', 'Confirmation median µs', 'Confirmation max/min'], body)
confirmation_text += '\n\n[Scalar benchstat](scalar-benchstat.txt), [SIMD benchstat](simd-benchstat.txt), and [selection and execution metadata](metadata.json).\n'
(root / 'confirmation' / 'report.md').write_text(confirmation_text)


def metric_rows(family, patterns, buckets, n=120, timing='regular'):
    result = []
    codecs = ('XOR', 'XOR2', 'ALP') if family == 'float' else ('legacy', 'ALPv3', 'ALPv4')
    for pattern in patterns:
        for b in buckets:
            for codec in codecs:
                e = get(family, pattern, n, b, timing, codec)
                d = get(family, pattern, n, b, timing, codec, 'decode')
                result.append([pattern, str(b), codec, fmt(e['encoded-B/sample'], 3),
                               fmt(e['numeric-compression-ratio']) + ':1',
                               fmt(e['ns/op'] / 1000, 3), fmt(d['ns/op'] / 1000, 3),
                               fmt(e['cpu-ns/sample']), fmt(d['cpu-ns/sample']),
                               str(int(e['B/op'])), str(int(e['allocs/op']))])
    return result


text = f'''# Comprehensive ALP compression benchmark

This report compares the current ALP implementation with XOR, XOR2, and the
existing integer and float histogram encodings. It measures complete encoding,
materialized decoding, CPU time, allocated memory, and stored size, including
experimental histogram v4. It also compares scalar and ARM64 SIMD execution.
The tables identify where faster decoding or smaller output requires more
encoding work; no single aggregate score is treated as a production result.

The run contains **7,728 observations**, **1,288 medians**, and **111 scenarios**,
with six repetitions at 250 ms per operation. It ran on {meta['cpu']},
{meta['platform']}, using Go 1.27.1 and source revision `{meta['revision']}`.
Started `{meta['started_utc']}`; completed `{meta['completed_utc']}`.
Recorded benchmark command durations total {process_seconds/60:.1f} minutes;
the UTC interval is {utc_seconds/60:.1f} minutes. Commands use a monotonic clock,
while start/end metadata uses UTC. The run does not diagnose any discrepancy
between those totals. Neither total is substituted for per-operation CPU cost.

## Main findings

'''
for family, codec, baseline in [('float', 'ALP', 'XOR'), ('float', 'ALP', 'XOR2'), ('integer-histogram', 'ALPv3', 'legacy'), ('integer-histogram', 'ALPv4', 'legacy'), ('float-histogram', 'ALPv3', 'legacy'), ('float-histogram', 'ALPv4', 'legacy')]:
    selected = [r for r in comparisons if r['backend'] == 'simd' and r['family'] == family and r['codec'] == codec and r['baseline'] == baseline]
    enc = [r for r in selected if r['operation'] == 'encode']
    dec = [r for r in selected if r['operation'] == 'decode']
    text += (f"- **{family}, {codec} versus {baseline}:** fewer stored bytes in {sum(r['size_ratio'] < 1 for r in enc)}/{len(enc)} scenarios; "
             f"warm decoding over 5% faster in {sum(r['speedup'] > 1.05 for r in dec)}/{len(dec)}; "
             f"encoding CPU over 5% higher in {sum(r['cpu_ratio'] > 1.05 for r in enc)}/{len(enc)}.\n")
e = get('float', 'decimal2', 120, 0, 'no-st', 'ALP')
ref = get('float', 'decimal2', 120, 0, 'no-st', 'XOR2')
d = get('float', 'decimal2', 120, 0, 'no-st', 'ALP', 'decode')
ref_d = get('float', 'decimal2', 120, 0, 'no-st', 'XOR2', 'decode')
text += (f"\nFor 120 two-decimal float samples, ALP stores {e['encoded-B/sample']:.3f} "
         f"bytes/sample versus XOR2's {ref['encoded-B/sample']:.3f}, decodes "
         f"{ref_d['ns/op']/d['ns/op']:.2f}× as fast, and uses "
         f"{e['cpu-ns/sample']/ref['cpu-ns/sample']:.2f}× the encoding CPU.\n")
v3 = get('float-histogram', 'smooth', 120, 128, 'regular', 'ALPv3')
v4 = get('float-histogram', 'smooth', 120, 128, 'regular', 'ALPv4')
v3d = get('float-histogram', 'smooth', 120, 128, 'regular', 'ALPv3', 'decode')
v4d = get('float-histogram', 'smooth', 120, 128, 'regular', 'ALPv4', 'decode')
text += (f"\nFor 128-bucket smooth float histograms, v4 reduces size from "
         f"{v3['encoded-B/sample']:.2f} to {v4['encoded-B/sample']:.2f} bytes/snapshot "
         f"relative to v3, while taking {v4['ns/op']/v3['ns/op']:.2f}× the encoding "
         f"time and {v4d['ns/op']/v3d['ns/op']:.2f}× the decoding time.\n")
text += '''

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

'''
headers = ['Pattern', 'Buckets', 'Codec', 'Encoded B/sample', 'Numeric ratio', 'Encode µs/batch', 'Decode µs/batch', 'Encode CPU ns/sample', 'Decode CPU ns/sample', 'Encode allocated B', 'Encode allocations']
float_patterns = ['constant', 'counter', 'decimal2', 'decimal6', 'noisy-decimal', 'computed', 'random-finite', 'random-bits', 'stale5', 'outliers20']
text += table(headers, metric_rows('float', float_patterns, [0], timing='no-st'))
text += '''

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

'''
body = []
for pattern in ('decimal2', 'computed'):
    for timing in ('no-st', 'no-st-jitter', 'regular', 'jitter', 'changing-st'):
        codecs = ('XOR', 'XOR2', 'ALP') if timing.startswith('no-st') else ('XOR2', 'ALP')
        for codec in codecs:
            e = get('float', pattern, 120, 0, timing, codec)
            d = get('float', pattern, 120, 0, timing, codec, 'decode')
            body.append([pattern, timing, codec, fmt(e['encoded-B/sample'], 3), fmt(e['ns/op']/1000, 3), fmt(d['ns/op']/1000, 3)])
text += table(['Pattern', 'Timestamps', 'Codec', 'B/sample', 'Encode µs/batch', 'Decode µs/batch'], body)
text += '''

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

'''
text += table(headers, metric_rows('integer-histogram', ['smooth', 'bursty', 'gauge', 'resets', 'layout', 'stale'], [8, 128]))
text += '''

## Float histograms

The existing codec is `EncFloatHistogramST`. Smooth and bursty fixtures have
integral-valued float bucket populations. Fractional fixtures divide cumulative
bucket counts by 100; noisy fractional fixtures add random fractions. This
separates the favorable temporal case from arbitrary floating-point counts.

The histogram numeric ratio uses count, zero count, sum, and all positive and
negative bucket values at 8 bytes each. It excludes raw timestamps, spans,
custom bounds, struct headers, pointers, and allocation overhead. Stale and
layout-changing snapshots use their actual fixture field counts.

'''
text += table(headers, metric_rows('float-histogram', ['smooth', 'bursty', 'fractional', 'noisy-fractional', 'gauge', 'resets', 'layout', 'stale'], [8, 128]))
text += '''

## Chunk size and very wide histograms

The full data includes 32, 120, and 1,024 samples. These selected results show
whether fixed costs amortize with larger input batches. Reset and layout cases
may create multiple chunks; timings and byte totals include every resulting
chunk, so batch size is not necessarily final physical chunk size.

'''
body = []
for family, pattern, buckets, timing, codecs in [
    ('float', 'decimal2', 0, 'no-st', ('XOR', 'XOR2', 'ALP')),
    ('float', 'random-bits', 0, 'no-st', ('XOR', 'XOR2', 'ALP')),
    ('integer-histogram', 'bursty', 8, 'regular', ('legacy', 'ALPv3', 'ALPv4')),
    ('float-histogram', 'smooth', 8, 'regular', ('legacy', 'ALPv3', 'ALPv4')),
]:
    for n in (32, 120, 1024):
        for codec in codecs:
            e = get(family, pattern, n, buckets, timing, codec)
            d = get(family, pattern, n, buckets, timing, codec, 'decode')
            body.append([family + '/' + pattern, str(n), codec, fmt(e['encoded-B/sample'], 3), fmt(e['M-samples/s']), fmt(d['M-samples/s']), fmt(e['cpu-ns/sample'])])
text += table(['Input', 'Samples', 'Codec', 'B/sample', 'Encode M samples/s', 'Decode M samples/s', 'Encode CPU ns/sample'], body)
text += '\n\nVery wide histograms have 1,031 total buckets, crossing the numeric vector boundary.\n\n'
body = []
for family in ('integer-histogram', 'float-histogram'):
    for codec in ('legacy', 'ALPv3', 'ALPv4'):
        e = get(family, 'smooth', 120, 1031, 'regular', codec)
        d = get(family, 'smooth', 120, 1031, 'regular', codec, 'decode')
        body.append([family, codec, fmt(e['encoded-B/sample']), fmt(e['ns/op'] / 1000), fmt(d['ns/op'] / 1000), fmt(e['cpu-ns/sample']), str(int(e['B/op']))])
text += table(['Input', 'Codec', 'B/snapshot', 'Encode µs/batch', 'Decode µs/batch', 'Encode CPU ns/snapshot', 'Allocated B/batch'], body)
text += '''

## SIMD versus scalar

Both builds use the same Go compiler and source. Only `GOEXPERIMENT=simd`
differs; the scalar build selects ALP's scalar fallback kernels. This measures
complete-codec effects of NEON, including scalar work
around vector kernels; it is not an isolated instruction benchmark. No AVX2
or AVX-512 performance is inferred from this ARM64 machine.

'''
body = []
for family, pattern, buckets, codec, timing in [
    ('float', 'decimal2', 0, 'ALP', 'no-st'), ('float', 'computed', 0, 'ALP', 'no-st'),
    ('float', 'random-bits', 0, 'ALP', 'no-st'),
    ('integer-histogram', 'smooth', 128, 'ALPv3', 'regular'),
    ('integer-histogram', 'bursty', 128, 'ALPv4', 'regular'),
    ('float-histogram', 'smooth', 128, 'ALPv3', 'regular'),
    ('float-histogram', 'smooth', 128, 'ALPv4', 'regular'),
    ('float-histogram', 'fractional', 128, 'ALPv3', 'regular'),
]:
    for op in ('encode', 'decode'):
        scalar = get(family, pattern, 120, buckets, timing, codec, op, 'scalar')
        simd = get(family, pattern, 120, buckets, timing, codec, op)
        body.append([family + '/' + pattern, str(buckets), codec, op, fmt(scalar['ns/op']/1000, 3), fmt(simd['ns/op']/1000, 3), fmt(scalar['ns/op']/simd['ns/op']) + '×', fmt(simd['cpu-ns/sample']/scalar['cpu-ns/sample']) + '×'])
text += table(['Input', 'Buckets', 'Codec', 'Operation', 'Scalar µs/batch', 'SIMD µs/batch', 'SIMD speedup', 'SIMD CPU/scalar CPU'], body)
text += '''

## Iterator allocation and histogram conversion

Fresh-iterator decoding creates iterator/output state for each scan of already
resident bytes. It does not flush CPU caches or read from disk. The separate
`decode-float` operation reads integer histograms as floating-point histograms,
including bucket conversion and materialization.

'''
body = []
for row in rows:
    if row['backend'] == 'simd' and row['suite'] == 'BenchmarkALPMatrix' and (row['operation'] == 'decode-cold' or row['operation'] == 'decode-float' and row['samples'] == 120 and row['buckets'] in (8, 128) and row['pattern'] in ('smooth', 'bursty')) and row['codec'] != 'ALPv1' and row['timing'] in ('regular', 'no-st'):
        body.append([row['family'] + '/' + row['pattern'], str(row['buckets']), row['timing'], row['codec'], row['operation'], fmt(row['ns/op']/1000, 3), fmt(row['cpu-ns/sample']), str(int(row['B/op'])), str(int(row['allocs/op']))])
text += table(['Input', 'Buckets', 'Time', 'Codec', 'Operation', 'Decode µs/batch', 'CPU ns/sample', 'Allocated B', 'Allocations'], body)
text += '''

## Adaptive compaction conversion

These measurements begin with an already encoded source chunk and reuse one
encoder for repeated chunks from a homogeneous series. They exclude initial
source encoding. Adaptive rejection includes amortized backoff and periodic
retries; it is not a cold first-decision latency. Forced conversion always
returns an ALP candidate, even if larger. Adaptive conversion requires at least
5% complete-size savings. Histogram adaptive conversion targets v3, not v4.
Throughput here counts input samples considered, including samples deliberately
skipped by rejection hints; it must not be read as a full decoding rate.

'''
body = []
for row in rows:
    if row['backend'] == 'simd' and row['suite'] == 'BenchmarkALPMatrixTranscode':
        body.append([row['family'] + '/' + row['pattern'], str(row['buckets']), row['codec'], fmt(row['ns/op']/1000, 3), fmt(row['cpu-ns/sample']), fmt(row['encoded-B/sample'], 3), fmt(row['accepted-%'], 1), str(int(row['B/op'])), str(int(row['allocs/op']))])
text += table(['Input', 'Buckets', 'Policy', 'Convert µs/batch', 'CPU ns/sample', 'Retained B/sample', 'Accepted %', 'Allocated B', 'Allocations'], body)
text += f'''

## CPU interpretation

Median process utilization across all rows is
{statistics.median(r['cpu-%core'] for r in rows):.1f}% of one core. A saturated
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
selected encoders and decoders. They ran on {profile_date} after the primary matrix;
their timings are excluded from the medians above. See the
[CPU hotspot tables and profiles](profiles/summary.md). Flat samples identify
work inside a function, while cumulative samples include its callees.
Some profiles attribute most samples to runtime functions such as `runtime.kevent`
({kevent_share} in the XOR2 encoding profile). The reason for that attribution was not
established, so these samples do not support precise codec hotspot conclusions.
The quantitative CPU comparisons above use process user/system accounting.

## Repetition variability

The primary matrix is preserved in full. {len(confirmation_meta['flagged'])}
rows had a slowest repetition more than 1.5 times the fastest. We repeated all
codecs and both builds for those affected case/operation pairs at 500 ms per
repetition, producing {confirmation_meta['observations']} additional measurements.
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
'''
(root / 'report.md').write_text(text)
# A standalone table remains useful offline and includes every operation.
template = (root / 'explorer-template.html').read_text()
(root / 'explorer.html').write_text(template.replace('__DATA__', json.dumps(rows)))
print(f'Validated {len(rows)} medians and generated report, comparisons, and explorer.')
