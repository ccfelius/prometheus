import json
import os
from pathlib import Path
import re
import resource
import statistics
import subprocess
import time

out = Path('/tmp/alp-xor2-20260929-093445')
env = dict(os.environ, GOMAXPROCS='1', GOGC='100')
results = []
for pi, pattern in enumerate(['constant', 'decimal', 'exceptions', 'computed', 'random']):
    for oi, operation in enumerate(['write', 'read']):
        codecs = ['XOR2', 'ALP'] if (pi + oi) % 2 == 0 else ['ALP', 'XOR2']
        for codec in codecs:
            command = ['/tmp/alp-xor2-benchmark.test', '-test.run=^$',
                       f'-test.bench=^BenchmarkALPChunk$/^{pattern}$/^{codec}$/^{operation}$',
                       '-test.benchmem', '-test.count=6', '-test.benchtime=1s', '-test.cpu=1']
            before = resource.getrusage(resource.RUSAGE_CHILDREN)
            start = time.monotonic()
            proc = subprocess.run(command, env=env, capture_output=True, text=True)
            elapsed = time.monotonic() - start
            after = resource.getrusage(resource.RUSAGE_CHILDREN)
            name = f'{pattern}-{codec}-{operation}'
            (out / f'{name}.txt').write_text(proc.stdout + proc.stderr)
            if proc.returncode:
                raise RuntimeError(proc.stdout + proc.stderr)
            samples = []
            for line in proc.stdout.splitlines():
                if not line.startswith('BenchmarkALPChunk/'):
                    continue
                tokens = line.split()
                metrics = {tokens[i + 1]: float(tokens[i]) for i in range(2, len(tokens), 2)}
                samples.append({'iterations': int(tokens[1]), **metrics})
            if len(samples) != 6:
                raise RuntimeError(f'Expected six results: {proc.stdout}')
            iterations = sum(row['iterations'] for row in samples)
            user = after.ru_utime - before.ru_utime
            system = after.ru_stime - before.ru_stime
            row = {'pattern': pattern, 'codec': codec, 'operation': operation,
                   'command': command, 'samples': samples, 'wall_seconds': elapsed,
                   'user_cpu_seconds': user, 'system_cpu_seconds': system,
                   'cpu_percent_of_one_core': 100 * (user + system) / elapsed,
                   'cpu_seconds_per_million_samples': (user + system) * 1e6 / (iterations * 120),
                   'median_ns_per_chunk': statistics.median(s['ns/op'] for s in samples),
                   'encoded_bytes_per_sample': samples[0]['encoded-B/sample']}
            results.append(row)
            (out / 'results.json').write_text(json.dumps(results, indent=2) + '\n')
            print(f"{name}: {row['median_ns_per_chunk']:.1f} ns/chunk, "
                  f"{row['cpu_seconds_per_million_samples']:.5f} CPU s/M samples, "
                  f"{row['cpu_percent_of_one_core']:.1f}% of one core", flush=True)
for codec in ['XOR2', 'ALP']:
    data = ''.join((out / f"{r['pattern']}-{codec}-{r['operation']}.txt").read_text().replace(f'/{codec}/', '/codec/')
                   for r in results if r['codec'] == codec)
    (out / f'{codec}.txt').write_text(data)
