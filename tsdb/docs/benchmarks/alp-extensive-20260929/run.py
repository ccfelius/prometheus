"""Run identical scalar/SIMD cases serially, with six observations per operation."""
import argparse
from collections import Counter
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import platform
import random
import re
import resource
import subprocess
import time


def parse(text):
    rows = []
    for line in text.splitlines():
        if not line.startswith('BenchmarkALPMatrix'):
            continue
        tokens = line.split()
        name = re.sub(r'-\d+$', '', tokens[0])
        metrics = {tokens[i + 1]: float(tokens[i]) for i in range(2, len(tokens), 2)}
        rows.append({'benchmark': name, 'iterations': int(tokens[1]), 'metrics': metrics})
    return rows


def command_output(command):
    return subprocess.check_output(command, text=True).strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--scalar', type=Path, required=True)
    parser.add_argument('--simd', type=Path, required=True)
    parser.add_argument('--out', type=Path, required=True)
    parser.add_argument('--smoke', type=Path, required=True)
    parser.add_argument('--count', type=int, default=6)
    parser.add_argument('--benchtime', default='150ms')
    parser.add_argument('--filter', default='')
    args = parser.parse_args()
    args.out.mkdir(parents=True, exist_ok=True)
    raw = args.out / 'raw'
    raw.mkdir(exist_ok=True)
    smoke = parse(args.smoke.read_text())
    expected = {}
    for row in smoke:
        group = '/'.join(row['benchmark'].split('/')[:6])
        expected.setdefault(group, set()).add(row['benchmark'])
    groups = sorted(g for g in expected if re.search(args.filter, g))
    if not groups:
        raise RuntimeError('No benchmark cases selected')
    random.Random(20260929).shuffle(groups)
    env = dict(os.environ, GOMAXPROCS='1', GOGC='100')
    binaries = {'scalar': args.scalar.resolve(), 'simd': args.simd.resolve()}
    metadata = {
        'started_utc': datetime.now(timezone.utc).isoformat(),
        'revision': command_output(['git', 'rev-parse', 'HEAD']),
        'git_status': command_output(['git', 'status', '--short']),
        'platform': platform.platform(), 'logical_cpus': os.cpu_count(),
        'environment': {'GOMAXPROCS': '1', 'GOGC': '100'},
        'repetitions': args.count, 'benchtime': args.benchtime,
        'case_order_seed': 20260929, 'case_order': groups,
        'binaries': {name: {'path': str(path), 'sha256': hashlib.sha256(path.read_bytes()).hexdigest(),
                           'go_version': command_output(['go', 'version', '-m', str(path)])}
                     for name, path in binaries.items()},
        'runner_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        'benchmark_source_sha256': hashlib.sha256(Path('tsdb/chunkenc/alp_matrix_bench_test.go').read_bytes()).hexdigest(),
    }
    if platform.system() == 'Darwin':
        metadata['cpu'] = command_output(['/usr/sbin/sysctl', '-n', 'machdep.cpu.brand_string'])
    (args.out / 'metadata.json').write_text(json.dumps(metadata, indent=2) + '\n')
    results, processes = [], []
    for gi, group in enumerate(groups):
        order = ['scalar', 'simd'] if gi % 2 == 0 else ['simd', 'scalar']
        for backend in order:
            pattern = '/'.join('^' + re.escape(part) + '$' for part in group.split('/'))
            command = [str(binaries[backend]), '-test.run=^$', '-test.bench=' + pattern,
                       '-test.benchmem', '-test.count=' + str(args.count),
                       '-test.benchtime=' + args.benchtime, '-test.cpu=1']
            before = resource.getrusage(resource.RUSAGE_CHILDREN)
            start = time.monotonic()
            proc = subprocess.run(command, env=env, capture_output=True, text=True, timeout=300)
            elapsed = time.monotonic() - start
            after = resource.getrusage(resource.RUSAGE_CHILDREN)
            text = proc.stdout + proc.stderr
            filename = f'{gi:03d}-{backend}.txt'
            (raw / filename).write_text(text)
            if proc.returncode:
                raise RuntimeError(f'{group} {backend}: {text}')
            rows = parse(text)
            counts = Counter(r['benchmark'] for r in rows)
            if set(counts) != expected[group] or set(counts.values()) != {args.count}:
                raise RuntimeError(f'Unexpected coverage: {group}: {counts}')
            for row in rows:
                if row['metrics'].get('cpu-ns/sample', 0) <= 0:
                    raise RuntimeError(f'CPU accounting unavailable: {row}')
                row.update(backend=backend, raw_file='raw/' + filename)
                results.append(row)
            processes.append({'case': group, 'backend': backend, 'command': command,
                              'wall_seconds': elapsed,
                              'user_cpu_seconds': after.ru_utime - before.ru_utime,
                              'system_cpu_seconds': after.ru_stime - before.ru_stime})
            (args.out / 'results.json').write_text(json.dumps(results, indent=2) + '\n')
            (args.out / 'processes.json').write_text(json.dumps(processes, indent=2) + '\n')
            print(f'{gi + 1}/{len(groups)} {backend}: {group} ({len(rows)} observations, {elapsed:.1f}s)', flush=True)
    for backend in binaries:
        lines = []
        for proc in processes:
            if proc['backend'] != backend:
                continue
            index = groups.index(proc['case'])
            text = (raw / f'{index:03d}-{backend}.txt').read_text()
            if not lines:
                lines.extend(l for l in text.splitlines() if l.startswith(('goos:', 'goarch:', 'pkg:', 'cpu:')))
            lines.extend(l for l in text.splitlines() if l.startswith('Benchmark'))
        (args.out / f'{backend}.txt').write_text('\n'.join(lines) + '\n')
    metadata['completed_utc'] = datetime.now(timezone.utc).isoformat()
    metadata['observations'] = len(results)
    (args.out / 'metadata.json').write_text(json.dumps(metadata, indent=2) + '\n')
    print(f'Complete: {len(results)} observations across {len(groups)} scenarios.', flush=True)


if __name__ == '__main__':
    main()
