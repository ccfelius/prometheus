"""Retain the full matrix and confirm comparisons with high repetition variability."""
import argparse
from collections import Counter, defaultdict
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import random
import re
import subprocess
import sys

sys.dont_write_bytecode = True
root = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('shared_runner', root.parent / 'alp-extensive-20260929' / 'run.py')
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)
p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--scalar', type=Path, required=True)
p.add_argument('--simd', type=Path, required=True)
a = p.parse_args()
meta = json.loads((root / 'metadata.json').read_text())
assert 'completed_utc' in meta
binaries = {'scalar':a.scalar.resolve(), 'simd':a.simd.resolve()}
for backend, binary in binaries.items():
    assert hashlib.sha256(binary.read_bytes()).hexdigest() == meta['binaries'][backend]['sha256']
primary = json.loads((root / 'results.json').read_text())
groups = defaultdict(list)
for row in primary:
    groups[row['backend'],row['benchmark']].append(row['metrics']['ns/op'])
flagged = []
cases = set()
for (backend, name), values in groups.items():
    ratio = max(values)/min(values)
    if ratio > 1.5:
        flagged.append({'backend':backend, 'benchmark':name, 'max_min_ratio':ratio})
        parts = name.split('/')
        cases.add(('/'.join(parts[:6]), parts[7] if len(parts)==8 else ''))
cases = sorted(cases)
random.Random(20261001).shuffle(cases)
out = root / 'confirmation'
(out / 'raw').mkdir(parents=True, exist_ok=True)
metadata = {'started_utc':datetime.now(timezone.utc).isoformat(), 'selection':'Primary ns/op maximum divided by minimum exceeds 1.5; all codecs and both builds for each affected case and operation.',
            'flagged':flagged, 'case_order':cases, 'count':6, 'benchtime':'500ms',
            'binaries':meta['binaries']}
(out / 'metadata.json').write_text(json.dumps(metadata,indent=2)+'\n')
results = []
headers = {}
for i,(case,operation) in enumerate(cases):
    expression = '/'.join('^'+re.escape(part)+'$' for part in case.split('/'))
    if operation:
        expression += '/.*/^'+re.escape(operation)+'$'
    names = {r['benchmark'] for r in primary if '/'.join(r['benchmark'].split('/')[:6])==case and (not operation or r['benchmark'].split('/')[-1]==operation)}
    for backend in (('scalar','simd') if i%2==0 else ('simd','scalar')):
        command = [str(binaries[backend]), '-test.run=^$', '-test.bench='+expression, '-test.count=6', '-test.benchtime=500ms', '-test.benchmem', '-test.cpu=1']
        proc = subprocess.run(command, env=dict(os.environ,GOMAXPROCS='1',GOGC='100'), capture_output=True, text=True, timeout=180)
        content = proc.stdout + proc.stderr
        filename = f'{i:03d}-{backend}.txt'
        (out / 'raw' / filename).write_text(content)
        if proc.returncode:
            raise RuntimeError(content)
        parsed = runner.parse(content)
        counts = Counter(r['benchmark'] for r in parsed)
        assert set(counts)==names and set(counts.values())=={6}, counts
        headers[backend] = [line for line in content.splitlines() if line.startswith(('goos:','goarch:','pkg:','cpu:'))]
        for row in parsed:
            row.update(backend=backend, raw_file='raw/'+filename)
            results.append(row)
        (out / 'results.json').write_text(json.dumps(results,indent=2)+'\n')
        print(f'{i+1}/{len(cases)} {backend}: {case}/{operation}',flush=True)
for backend in binaries:
    names = {r['benchmark'] for r in results if r['backend']==backend}
    original = [line for line in (root/f'{backend}.txt').read_text().splitlines() if line.startswith('Benchmark') and re.sub(r'-\d+$','',line.split()[0]) in names]
    (out/f'{backend}-primary.txt').write_text('\n'.join(headers.get(backend,[])+original)+'\n')
    measured = []
    for path in sorted((out/'raw').glob(f'*-{backend}.txt')):
        measured.extend(line for line in path.read_text().splitlines() if line.startswith('Benchmark'))
    (out/f'{backend}-confirmation.txt').write_text('\n'.join(headers.get(backend,[])+measured)+'\n')
metadata['completed_utc'] = datetime.now(timezone.utc).isoformat()
metadata['observations'] = len(results)
(out/'metadata.json').write_text(json.dumps(metadata,indent=2)+'\n')
print(f'Complete: {len(flagged)} flagged rows, {len(cases)} comparisons, {len(results)} confirmation observations.',flush=True)
