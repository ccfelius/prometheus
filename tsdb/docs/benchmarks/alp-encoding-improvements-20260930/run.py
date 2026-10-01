"""Compare frozen before/after SIMD binaries serially with alternating case order."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import random
import re
import subprocess

p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--before', type=Path, required=True)
p.add_argument('--after', type=Path, required=True)
p.add_argument('--out', type=Path, default=Path(__file__).resolve().parent)
a = p.parse_args()
a.out.mkdir(parents=True, exist_ok=True)
raw = a.out / 'raw'
raw.mkdir(exist_ok=True)
binaries = {'before': a.before.resolve(), 'after': a.after.resolve()}
groups = []
for timing in ('regular', 'no-st'):
    for pattern in ('constant', 'decimal2', 'decimal6', 'computed', 'random-bits'):
        groups.append(('BenchmarkALPMatrix', 'float', pattern, 'n=120', 'buckets=0', 'time='+timing))
for family, patterns in [('integer-histogram', ('smooth', 'bursty')), ('float-histogram', ('smooth', 'bursty', 'fractional', 'noisy-fractional'))]:
    for pattern in patterns:
        for buckets in (8, 128):
            groups.append(('BenchmarkALPMatrix', family, pattern, 'n=120', 'buckets='+str(buckets), 'time=regular'))
for family, pattern, buckets in [('float','decimal2',0), ('float','computed',0), ('float','random-bits',0), ('integer-histogram','smooth',8), ('integer-histogram','bursty',128), ('float-histogram','fractional',8), ('float-histogram','noisy-fractional',128)]:
    groups.append(('BenchmarkALPMatrixTranscode', family, pattern, 'n=120', 'buckets='+str(buckets), 'time=regular'))
random.Random(20261001).shuffle(groups)
env = dict(os.environ, GOMAXPROCS='1', GOGC='100')
meta = {'started_utc': datetime.now(timezone.utc).isoformat(),
        'revision': subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip(),
        'status_at_start': subprocess.check_output(['git','status','--short'],text=True).strip(),
        'environment': {'GOMAXPROCS':'1','GOGC':'100'}, 'count':6, 'benchtime':'150ms',
        'case_order':groups, 'binaries':{k:{'path':str(v),'sha256':hashlib.sha256(v.read_bytes()).hexdigest(),
            'build':subprocess.check_output(['go','version','-m',str(v)],text=True)} for k,v in binaries.items()}}
(a.out/'metadata.json').write_text(json.dumps(meta,indent=2)+'\n')
outputs = {'before':[], 'after':[]}
for index, group in enumerate(groups):
    pattern = '/'.join('^'+re.escape(x)+'$' for x in group)
    if group[0] == 'BenchmarkALPMatrix':
        pattern += '/^(ALP|ALPv3|ALPv4|XOR2|legacy)$/^(encode|decode)$'
    for backend in (['before','after'] if index%2==0 else ['after','before']):
        command=[str(binaries[backend]),'-test.run=^$','-test.bench='+pattern,'-test.benchmem','-test.count=6','-test.benchtime=150ms','-test.cpu=1']
        proc=subprocess.run(command,env=env,text=True,capture_output=True,timeout=180)
        output=proc.stdout+proc.stderr
        (raw/f'{index:02d}-{backend}.txt').write_text(output)
        if proc.returncode: raise RuntimeError(output)
        rows=[line for line in output.splitlines() if line.startswith('Benchmark')]
        counts={name:sum(line.split()[0]==name for line in rows) for name in {line.split()[0] for line in rows}}
        expected=2 if group[0]=='BenchmarkALPMatrixTranscode' else 6 if backend=='after' and group[1]!='float' else 4
        if len(counts)!=expected or set(counts.values())!={6}: raise RuntimeError(str(counts))
        if not outputs[backend]: outputs[backend].extend(line for line in output.splitlines() if line.startswith(('goos:','goarch:','pkg:','cpu:')))
        outputs[backend].extend(rows)
        (a.out/f'paired-{backend}.txt').write_text('\n'.join(outputs[backend])+'\n')
        print(f'{index+1}/{len(groups)} {backend}: {"/".join(group)}',flush=True)
meta['completed_utc']=datetime.now(timezone.utc).isoformat()
(a.out/'metadata.json').write_text(json.dumps(meta,indent=2)+'\n')
print('Complete.',flush=True)
