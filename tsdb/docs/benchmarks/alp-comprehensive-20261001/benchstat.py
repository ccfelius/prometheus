"""Generate pinned six-observation statistical comparisons after the timed run."""
import os
from pathlib import Path
import subprocess

root = Path(__file__).resolve().parent
command = ['go', 'run', 'golang.org/x/perf/cmd/benchstat@v0.0.0-20260908200009-22c9c6c9d4da']
env = dict(os.environ, GOTOOLCHAIN='go1.27.1')
for backend in ('scalar', 'simd'):
    for family, codecs in [('floats', ('xor', 'xor2', 'alp')), ('histograms', ('legacy', 'alpv3', 'alpv4'))]:
        with (root / f'{backend}-{family}-benchstat.txt').open('w') as output:
            subprocess.run(command + [f'{backend}-{c}.txt' for c in codecs], cwd=root, env=env, stdout=output, check=True)
    with (root / f'{backend}-xor2-alp-benchstat.txt').open('w') as output:
        subprocess.run(command + [f'{backend}-xor2.txt', f'{backend}-alp.txt'], cwd=root, env=env, stdout=output, check=True)
with (root / 'scalar-simd-benchstat.txt').open('w') as output:
    subprocess.run(command + ['scalar.txt', 'simd.txt'], cwd=root, env=env, stdout=output, check=True)
for backend in ('scalar', 'simd'):
    directory = root / 'confirmation'
    with (directory / f'{backend}-benchstat.txt').open('w') as output:
        subprocess.run(command + [f'{backend}-primary.txt', f'{backend}-confirmation.txt'], cwd=directory, env=env, stdout=output, check=True)
print('Wrote seven primary and two confirmation benchstat comparisons.')
