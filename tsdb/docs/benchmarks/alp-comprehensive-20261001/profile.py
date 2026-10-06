"""Collect separate CPU profiles after completing the unprofiled timed matrix."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess

root = Path(__file__).resolve().parent
p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--binary', type=Path, required=True)
a = p.parse_args()
binary = a.binary.resolve()
meta = json.loads((root / 'metadata.json').read_text())
assert 'completed_utc' in meta, 'Do not profile while the primary matrix is running.'
assert hashlib.sha256(binary.read_bytes()).hexdigest() == meta['binaries']['simd']['sha256']
out = root / 'profiles'
out.mkdir(exist_ok=True)
env = dict(os.environ, GOMAXPROCS='1', GOGC='100', GOTOOLCHAIN='go1.27.1')
pprof = ['go', 'run', 'github.com/google/pprof@v0.0.0-20260906184651-6331bc6350fe']
cases = [
    ('float', 'decimal2', 0, 'no-st', 'XOR2', 'encode'),
    ('float', 'decimal2', 0, 'no-st', 'ALP', 'encode'),
    ('float', 'decimal2', 0, 'no-st', 'ALP', 'decode'),
    ('integer-histogram', 'bursty', 128, 'regular', 'ALPv4', 'encode'),
    ('float-histogram', 'noisy-fractional', 128, 'regular', 'ALPv3', 'encode'),
    ('float-histogram', 'smooth', 128, 'regular', 'ALPv4', 'decode'),
]
records = []
summary = ['# Sampled CPU profiles', '',
           'These separate five-second SIMD profiling runs provide diagnostic samples. '
           'They run after the unprofiled matrix, and their timings are excluded '
           'from the report medians. Profiles cover the whole test process, including '
           'startup and setup. Flat percentages count samples attributed directly '
           'to a function; cumulative percentages also include its callees. '
           'Runtime-dominated samples do not establish precise codec hotspots; '
           'use process user/system CPU measurements for quantitative cost comparisons.', '']
for family, pattern, buckets, timing, codec, operation in cases:
    parts = ['BenchmarkALPMatrix', family, pattern, 'n=120', f'buckets={buckets}', f'time={timing}', codec, operation]
    name = f'{family}-{pattern}-{codec}-{operation}'
    profile = out / f'{name}.pprof'
    command = [str(binary), '-test.run=^$', '-test.bench=' + '/'.join('^'+re.escape(x)+'$' for x in parts),
               '-test.benchtime=5s', '-test.count=1', '-test.cpu=1', '-test.cpuprofile='+str(profile)]
    with (out / f'{name}.txt').open('w') as output:
        subprocess.run(command, env=env, stdout=output, stderr=subprocess.STDOUT, check=True)
    for kind, options in [('flat', []), ('cumulative', ['-cum'])]:
        with (out / f'{name}-{kind}.txt').open('w') as output:
            subprocess.run(pprof + ['-top', '-nodecount=15'] + options + [str(profile)], env=env, stdout=output, check=True)
    records.append({'case':'/'.join(parts), 'profile':str(profile.relative_to(root)), 'command':command})
    summary.extend(['## ' + family.replace('-', ' ') + ' ' + pattern.replace('-', ' ') + ' ' + codec + ' ' + operation, '',
                    '| Function | Flat CPU samples | Cumulative CPU samples |', '| --- | --- | --- |'])
    found = 0
    for line in (out / f'{name}-flat.txt').read_text().splitlines():
        fields = line.split()
        if len(fields) >= 6 and re.fullmatch(r'\d+(?:\.\d+)?%', fields[1]) and fields[4].endswith('%'):
            function = ' '.join(fields[5:]).replace('github.com/prometheus/prometheus/', '')
            summary.append(f'| `{function}` | {fields[1]} | {fields[4]} |')
            found += 1
            if found == 5:
                break
    assert found > 0, name
    summary.extend(['', f'[Flat profile]({name}-flat.txt), [cumulative profile]({name}-cumulative.txt), and [raw CPU profile]({name}.pprof).', ''])
    print(name, 'profiled', flush=True)
(out / 'metadata.json').write_text(json.dumps({'completed_utc':datetime.now(timezone.utc).isoformat(), 'binary_sha256':meta['binaries']['simd']['sha256'], 'analyzer':pprof, 'records':records}, indent=2)+'\n')
(out / 'summary.md').write_text('\n'.join(summary))
