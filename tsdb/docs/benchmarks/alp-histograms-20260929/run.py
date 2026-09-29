import json
import os
from pathlib import Path
import resource
import statistics
import subprocess
import time

out = Path('/tmp/alp-histograms-20260929-095803')
env = dict(os.environ, GOMAXPROCS='1', GOGC='100')
results = []
cases = [('integer', 'legacy', 'histogramST'), ('integer', 'ALPv1', 'ALPHistogram'),
         ('integer', 'ALPv2', 'ALPHistogramV2'), ('float', 'legacy', 'floathistogramST'),
         ('float', 'ALPv1', 'ALPFloatHistogram')]
for family, variant, codec in cases:
    for operation in ['write', 'read']:
        command = ['/tmp/alp-xor2-benchmark.test', '-test.run=^$',
                   f'-test.bench=^BenchmarkALPHistograms$/^{codec}$/^{operation}$',
                   '-test.benchmem', '-test.count=6', '-test.benchtime=1s', '-test.cpu=1']
        before = resource.getrusage(resource.RUSAGE_CHILDREN)
        start = time.monotonic()
        proc = subprocess.run(command, env=env, capture_output=True, text=True)
        elapsed = time.monotonic() - start
        after = resource.getrusage(resource.RUSAGE_CHILDREN)
        (out / f'{codec}-{operation}.txt').write_text(proc.stdout + proc.stderr)
        if proc.returncode:
            raise RuntimeError(proc.stdout + proc.stderr)
        samples = []
        for line in proc.stdout.splitlines():
            if not line.startswith('BenchmarkALPHistograms/'):
                continue
            tokens = line.split()
            metrics = {tokens[i+1]: float(tokens[i]) for i in range(2, len(tokens), 2)}
            samples.append({'iterations': int(tokens[1]), **metrics})
        if len(samples) != 6:
            raise RuntimeError(f'Expected six results: {proc.stdout}')
        iterations = sum(s['iterations'] for s in samples)
        user, system = after.ru_utime-before.ru_utime, after.ru_stime-before.ru_stime
        row = {'family': family, 'variant': variant, 'codec': codec, 'operation': operation,
               'command': command, 'samples': samples, 'wall_seconds': elapsed,
               'user_cpu_seconds': user, 'system_cpu_seconds': system,
               'cpu_percent_of_one_core': 100*(user+system)/elapsed,
               'cpu_seconds_per_million_samples': (user+system)*1e6/(iterations*120),
               'median_ns_per_chunk': statistics.median(s['ns/op'] for s in samples),
               'encoded_bytes_per_sample': samples[0]['encoded-B/sample']}
        results.append(row)
        (out/'results.json').write_text(json.dumps(results, indent=2)+'\n')
        print(f"{codec}-{operation}: {row['median_ns_per_chunk']:.1f} ns/chunk, "
              f"{row['encoded_bytes_per_sample']:.4f} B/sample, "
              f"{row['cpu_seconds_per_million_samples']:.5f} CPU s/M histograms", flush=True)
for variant in ['legacy', 'ALPv1', 'ALPv2']:
    data = ''.join((out/f"{r['codec']}-{r['operation']}.txt").read_text().replace(
                   f"/{r['codec']}/", f"/{r['family']}/codec/")
                   for r in results if r['variant']==variant)
    (out/f'{variant}.txt').write_text(data)
