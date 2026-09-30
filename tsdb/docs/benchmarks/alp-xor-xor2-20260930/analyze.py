"""Generate a three-codec report from the shared matrix runner's measurements."""
import importlib.util
import json
from pathlib import Path
import statistics
import sys

sys.dont_write_bytecode = True
root = Path(__file__).resolve().parent
shared_path = root.parent / 'alp-extensive-20260929' / 'analyze.py'
spec = importlib.util.spec_from_file_location('matrix_analysis', shared_path)
shared = importlib.util.module_from_spec(spec)
spec.loader.exec_module(shared)
rows = shared.summarize(root)
index = {(r['backend'], r['pattern'], r['samples'], r['timing'], r['codec'], r['operation']): r for r in rows}
metadata = json.loads((root / 'metadata.json').read_text())


def get(pattern, codec, operation, backend='simd', n=120, timing='no-st'):
    return index[backend, pattern, n, timing, codec, operation]


def table(headers, body):
    return '\n'.join(['| ' + ' | '.join(headers) + ' |', '| ' + ' | '.join(['---'] * len(headers)) + ' |'] + ['| ' + ' | '.join(row) + ' |' for row in body])


for backend in ('scalar', 'simd'):
    raw = (root / f'{backend}.txt').read_text().splitlines()
    headers = [line for line in raw if line.startswith(('goos:', 'goarch:', 'pkg:', 'cpu:'))]
    for codec in ('XOR', 'XOR2', 'ALP'):
        lines = [line.replace(f'/{codec}/', '/codec/') for line in raw if line.startswith('Benchmark') and f'/{codec}/' in line]
        (root / f'{backend}-{codec.lower()}.txt').write_text('\n'.join(headers + lines) + '\n')

text = f'''# ALP compared with XOR and XOR2

This benchmark directly compares the current ALP implementation with both
existing float encodings, XOR and XOR2, on identical input values and timestamps.
All start timestamps are zero because XOR cannot represent them. The earlier
[extensive benchmark](../alp-extensive-20260929/report.md) compares XOR2 and ALP
with nonzero start timestamps and also covers histogram codecs.

Measured {metadata['started_utc'][:10]} on {metadata['cpu']}, darwin/arm64,
Go 1.27.1. Source revision: `{metadata['revision']}`.
There are {metadata['observations']:,} observations across 32 input scenarios,
two builds, three codecs, and six repetitions of each operation. The scalar and
SIMD builds use the same compiler; only `GOEXPERIMENT=simd` differs.

## Main findings

'''
for baseline in ('XOR', 'XOR2'):
    pairs = []
    for r in rows:
        if r['backend'] == 'simd' and r['codec'] == baseline and r['operation'] == 'encode':
            alp = get(r['pattern'], 'ALP', 'encode', n=r['samples'], timing=r['timing'])
            dec = get(r['pattern'], baseline, 'decode', n=r['samples'], timing=r['timing'])
            alp_dec = get(r['pattern'], 'ALP', 'decode', n=r['samples'], timing=r['timing'])
            pairs.append((dec['ns/op']/alp_dec['ns/op'], alp['cpu-ns/sample']/r['cpu-ns/sample'], alp['encoded-B/sample']/r['encoded-B/sample']))
    text += (f"- Against **{baseline}**, SIMD ALP warm decoding is faster in **{sum(p[0]>1 for p in pairs)}/{len(pairs)}** cases "
             f"({min(p[0] for p in pairs):.2f}–{max(p[0] for p in pairs):.2f}× speedup). "
             f"Encoding consumes more CPU in **{sum(p[1]>1 for p in pairs)}/{len(pairs)}** cases. "
             f"ALP stores fewer bytes in **{sum(p[2]<1 for p in pairs)}/{len(pairs)}** cases.\n")
text += f'''
Median process CPU utilization across rows is
{statistics.median(r['cpu-%core'] for r in rows):.1f}% of one core. Compare CPU
time for a fixed amount of data to assess CPU savings. Utilization alone does
not distinguish a fast codec from a slow one when both continuously run.

These are synthetic, memory-resident full-codec measurements. They do not
establish production ingestion or query performance, and no AMD64 machine was
tested. Case counts do not represent the frequency of patterns in production.

## Compression and decompression speed

SIMD build, 120 samples per chunk, regular timestamps. Times are microseconds
for the complete chunk; lower is better. Encoding includes appending and final
serialization. Warm decoding reuses an iterator and materializes every value.

'''
patterns = ['constant', 'counter', 'decimal2', 'decimal6', 'noisy-decimal', 'computed', 'random-finite', 'random-bits', 'stale5', 'outliers20']
body = []
for pattern in patterns:
    for codec in ('XOR', 'XOR2', 'ALP'):
        e, d = get(pattern, codec, 'encode'), get(pattern, codec, 'decode')
        body.append([pattern, codec, f"{e['ns/op']/1000:.3f}", f"{d['ns/op']/1000:.3f}", f"{e['M-samples/s']:.1f}", f"{d['M-samples/s']:.1f}"])
text += table(['Pattern', 'Codec', 'Encode µs/chunk', 'Decode µs/chunk', 'Encode M samples/s', 'Decode M samples/s'], body)
text += '''

## CPU cost and compressed size

CPU time is user plus system process CPU, including runtime and GC work in the
timed loop. CPU ns/sample is numerically equal to milliseconds per million
samples. Encoded bytes include chunk headers and timestamp streams. The numeric
compression ratio uses the 8-byte float value as its uncompressed baseline, excluding the
raw timestamp size; it is `8 / encoded bytes per sample` for all three codecs.

'''
body = []
for pattern in patterns:
    for codec in ('XOR', 'XOR2', 'ALP'):
        e, d = get(pattern, codec, 'encode'), get(pattern, codec, 'decode')
        body.append([pattern, codec, f"{e['cpu-ns/sample']:.2f}", f"{d['cpu-ns/sample']:.2f}", f"{e['encoded-B/sample']:.3f}", f"{8/e['encoded-B/sample']:.2f}:1"])
text += table(['Pattern', 'Codec', 'Encode CPU ns/sample', 'Decode CPU ns/sample', 'Encoded B/sample', 'Numeric compression ratio'], body)
text += '''

## Cold iterator decoding

These full scans allocate a fresh iterator for each chunk. The encoded bytes
remain in memory, so this is an allocation comparison, not a cold disk or CPU
cache measurement. All cases below have 120 samples and regular timestamps.

'''
body = []
for pattern in ('decimal2', 'random-bits'):
    for codec in ('XOR', 'XOR2', 'ALP'):
        r = get(pattern, codec, 'decode-cold')
        body.append([pattern, codec, f"{r['ns/op']/1000:.3f}", f"{r['cpu-ns/sample']:.2f}", str(int(r['B/op'])), str(int(r['allocs/op']))])
text += table(['Pattern', 'Codec', 'Decode µs/chunk', 'CPU ns/sample', 'Allocated B/chunk', 'Allocations/chunk'], body)
text += '''

## Methodology and reproduction

The 32 scenarios contain ten patterns at 32, 120, and 1024 samples, plus two
irregular-timestamp cases at 120 samples. The fixture seed is 20260929. Both
builds use `GOMAXPROCS=1`, `GOGC=100`, `-test.cpu=1`, `-test.count=6`, and
`-test.benchtime=150ms`. Scenarios run serially in deterministic shuffled order;
the first scalar/SIMD build alternates between scenarios. Each codec receives
identical float bits and timestamps, with start timestamps set to zero.

Correctness tests compare all decoded float bits, timestamps, and start
timestamps, including NaN payloads. They also check deterministic encoded bytes.
These tests passed in both scalar and SIMD builds. The SIMD lint configuration
has an existing formatter disagreement over the `simd/archsimd` import in
`alp_encode_arm64.go`: gofumpt requires it alongside standard imports, while
gci requires a separate group. This benchmark does not change that source.
The shared [methodology](../alp-extensive-20260929/methodology.md) documents
fixture definitions, timing boundaries, and CPU measurement details.

All operations, including other chunk sizes, irregular timestamps, scalar
results, allocations, and per-row CPU utilization, are in [summary.csv](summary.csv).
The [raw observations](results.json), [run metadata](metadata.json), and
[process diagnostics](processes.json) preserve the inputs to this report.
Six-run benchstat comparisons are available for the [SIMD build](simd-benchstat.txt)
and [scalar build](scalar-benchstat.txt). The analysis script normalizes codec
names so benchstat compares identical cases across the three files.

Run from the repository root:

```sh
GOTOOLCHAIN=go1.27.1 GOEXPERIMENT='' go test -c ./tsdb/chunkenc -o /tmp/alp-three-scalar.test
GOTOOLCHAIN=go1.27.1 GOEXPERIMENT=simd go test -c ./tsdb/chunkenc -o /tmp/alp-three-simd.test
GOMAXPROCS=1 GOGC=100 /tmp/alp-three-simd.test -test.run='^TestALPMatrixFixtures$' -test.bench='^BenchmarkALPMatrix$/^float$/.*/.*/.*/^time=no-st' -test.benchtime=1x -test.count=1 -test.cpu=1 > /tmp/alp-three-smoke.txt
GOMAXPROCS=1 /tmp/alp-three-scalar.test -test.run='^TestALPMatrixFixtures$'
python3 tsdb/docs/benchmarks/alp-extensive-20260929/run.py --scalar /tmp/alp-three-scalar.test --simd /tmp/alp-three-simd.test --smoke /tmp/alp-three-smoke.txt --out tsdb/docs/benchmarks/alp-xor-xor2-20260930 --filter '^BenchmarkALPMatrix/float/.*time=no-st' --count 6 --benchtime 150ms
python3 tsdb/docs/benchmarks/alp-xor-xor2-20260930/analyze.py
cd tsdb/docs/benchmarks/alp-xor-xor2-20260930
GOTOOLCHAIN=go1.27.1 go run golang.org/x/perf/cmd/benchstat@v0.0.0-20260908200009-22c9c6c9d4da simd-xor.txt simd-xor2.txt simd-alp.txt > simd-benchstat.txt
GOTOOLCHAIN=go1.27.1 go run golang.org/x/perf/cmd/benchstat@v0.0.0-20260908200009-22c9c6c9d4da scalar-xor.txt scalar-xor2.txt scalar-alp.txt > scalar-benchstat.txt
```

The one-iteration smoke run enumerates the cases and is excluded from the
measurements. Regenerating statistics does not require repeating measurements.
'''
(root / 'report.md').write_text(text)
print(f'Wrote {len(rows)} medians and the XOR/XOR2/ALP report.')
