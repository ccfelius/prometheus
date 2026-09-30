"""Summarize the six-run matrix into CSV, comparison tables, and an HTML explorer."""
import argparse
from collections import defaultdict
import csv
import json
from pathlib import Path
import statistics


def summarize(out):
    groups = defaultdict(list)
    for row in json.loads((out / 'results.json').read_text()):
        groups[(row['backend'], row['benchmark'])].append(row)
    summary = []
    for (backend, name), observations in sorted(groups.items()):
        parts = name.split('/')
        entry = dict(backend=backend, suite=parts[0], family=parts[1], pattern=parts[2],
                     samples=int(parts[3].split('=')[1]), buckets=int(parts[4].split('=')[1]),
                     timing=parts[5].split('=')[1], codec=parts[6],
                     operation=parts[7] if len(parts) == 8 else 'transcode',
                     observations=len(observations), benchmark=name)
        for metric in observations[0]['metrics']:
            numbers = [o['metrics'][metric] for o in observations]
            entry[metric] = statistics.median(numbers)
        times = [o['metrics']['ns/op'] for o in observations]
        entry['min-ns/op'], entry['max-ns/op'] = min(times), max(times)
        entry['M-samples/s'] = entry['samples'] * 1000 / entry['ns/op']
        entry['cpu-seconds/M-samples'] = entry['cpu-ns/sample'] / 1000
        summary.append(entry)
    (out / 'summary.json').write_text(json.dumps(summary, indent=2) + '\n')
    columns = ['backend', 'suite', 'family', 'pattern', 'samples', 'buckets', 'timing', 'codec',
               'operation', 'observations', 'ns/op', 'min-ns/op', 'max-ns/op', 'M-samples/s',
               'MB/s', 'encoded-B/sample', 'cpu-seconds/M-samples', 'cpu-ns/sample',
               'user-cpu-ns/sample', 'sys-cpu-ns/sample', 'cpu-%core', 'B/op', 'allocs/op',
               'chunks/op', 'accepted-%', 'benchmark']
    with (out / 'summary.csv').open('w', newline='') as stream:
        writer = csv.DictWriter(stream, fieldnames=columns, extrasaction='ignore', lineterminator='\n')
        writer.writeheader()
        writer.writerows(summary)
    return summary


def pairs(summary):
    indexed = defaultdict(dict)
    for row in summary:
        if row['suite'] != 'BenchmarkALPMatrix' or row['codec'] == 'ALPv1':
            continue
        key = tuple(row[k] for k in ['backend', 'family', 'pattern', 'samples', 'buckets', 'timing', 'operation'])
        indexed[key][row['codec']] = row
    result = []
    for key, codecs in sorted(indexed.items()):
        baseline = codecs.get('XOR2', codecs.get('legacy'))
        alp = codecs.get('ALP', codecs.get('ALPv3'))
        if baseline is None or alp is None:
            continue
        result.append(dict(zip(['backend', 'family', 'pattern', 'samples', 'buckets', 'timing', 'operation'], key),
                           baseline=baseline, alp=alp,
                           throughput_speedup=baseline['ns/op'] / alp['ns/op'],
                           cpu_cost_ratio=alp['cpu-ns/sample'] / baseline['cpu-ns/sample'],
                           size_ratio=alp['encoded-B/sample'] / baseline['encoded-B/sample']))
    return result


def write_benchstat_inputs(out):
    for backend in ['scalar', 'simd']:
        lines = (out / (backend + '.txt')).read_text().splitlines()
        header = [l for l in lines if l.startswith(('goos:', 'goarch:', 'pkg:', 'cpu:'))]
        baseline, alp = header[:], header[:]
        for line in lines:
            if not line.startswith('BenchmarkALPMatrix/'):
                continue
            if '/XOR2/' in line or '/legacy/' in line:
                baseline.append(line.replace('/XOR2/', '/codec/').replace('/legacy/', '/codec/'))
            elif '/ALP/' in line or '/ALPv3/' in line:
                alp.append(line.replace('/ALP/', '/codec/').replace('/ALPv3/', '/codec/'))
        (out / f'{backend}-legacy.txt').write_text('\n'.join(baseline) + '\n')
        (out / f'{backend}-alp.txt').write_text('\n'.join(alp) + '\n')


def markdown_tables(out, summary, comparisons):
    text = ['# Detailed benchmark medians', '',
            'All rates are per input observation: one float or one whole histogram. CPU cost is process user + system CPU seconds per million observations. Decode materializes values; integer `decode-float` produces float histograms. Six repetitions per row.', '']
    for family in ['float', 'integer-histogram', 'float-histogram']:
        text += ['## ' + family + ': SIMD, 120 samples, regular timestamps', '',
                 '| Pattern | Buckets | Bytes/sample legacy → ALP | Encode M samples/s legacy → ALP | Decode M samples/s legacy → ALP | Encode CPU s/M legacy → ALP | Decode CPU s/M legacy → ALP |',
                 '| --- | ---: | ---: | ---: | ---: | ---: | ---: |']
        bycase = defaultdict(dict)
        for p in comparisons:
            if p['backend'] == 'simd' and p['family'] == family and p['samples'] == 120 and p['timing'] == 'regular':
                bycase[(p['pattern'], p['buckets'])][p['operation']] = p
        for (pattern, buckets), ops in sorted(bycase.items()):
            e, d = ops['encode'], ops['decode']
            def both(pair, field, digits=3):
                return f"{pair['baseline'][field]:.{digits}f} → {pair['alp'][field]:.{digits}f}"
            text.append(f"| {pattern} | {buckets} | {both(e, 'encoded-B/sample')} | {both(e, 'M-samples/s')} | {both(d, 'M-samples/s')} | {both(e, 'cpu-seconds/M-samples', 4)} | {both(d, 'cpu-seconds/M-samples', 4)} |")
        text.append('')
    text += ['## Incremental compaction conversion: SIMD', '',
             'Source chunks already exist. These measurements exclude initial legacy encoding and reuse one encoder for a repeated series. Adaptive floats include periodic retry hints; adaptive histograms still perform a full trial before the size gate.', '',
             '| Family / pattern | Buckets | Policy | M samples/s | CPU s/M samples | CPU % of one core | Bytes/sample retained | Accepted % |',
             '| --- | ---: | --- | ---: | ---: | ---: | ---: | ---: |']
    for r in summary:
        if r['suite'] == 'BenchmarkALPMatrixTranscode' and r['backend'] == 'simd':
            text.append(f"| {r['family']} / {r['pattern']} | {r['buckets']} | {r['codec']} | {r['M-samples/s']:.3f} | {r['cpu-seconds/M-samples']:.4f} | {r['cpu-%core']:.1f} | {r['encoded-B/sample']:.3f} | {r['accepted-%']:.1f} |")
    text += ['', '## All codec comparisons', '',
             'Speedup above 1 means ALP processes more observations per second. CPU and size ratios below 1 favor ALP. These are ratios of medians, not aggregate production workload results.', '',
             '| Build | Family | Pattern | Samples | Buckets | Time | Operation | ALP speedup | ALP CPU / legacy CPU | ALP bytes / legacy bytes |',
             '| --- | --- | --- | ---: | ---: | --- | --- | ---: | ---: | ---: |']
    for p in comparisons:
        text.append(f"| {p['backend']} | {p['family']} | {p['pattern']} | {p['samples']} | {p['buckets']} | {p['timing']} | {p['operation']} | {p['throughput_speedup']:.3f}× | {p['cpu_cost_ratio']:.3f}× | {p['size_ratio']:.3f}× |")
    (out / 'tables.md').write_text('\n'.join(text) + '\n')


def explorer(out, summary):
    template = '''<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>ALP compression benchmarks</title>
<style>
body{font:15px system-ui,sans-serif;color:#172536;background:#f5f7fa;margin:0}main{max-width:1250px;margin:auto;padding:28px}h1{font-size:28px}p{line-height:1.5;color:#485769}.controls{display:flex;gap:16px;flex-wrap:wrap;background:white;padding:18px;border-radius:10px}label{display:grid;gap:5px}select{padding:7px;border:1px solid #bbc6d2;border-radius:5px}#charts{display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:16px;margin-top:20px}.chart{background:white;padding:18px;border-radius:10px}h2{font-size:17px}.barrow{margin:17px 0}.pattern{font-size:13px;margin-bottom:5px}.bar{min-width:2px;height:17px;margin:3px 0;border-radius:2px}.legend{display:flex;gap:20px;margin:16px 0}.swatch{display:inline-block;width:13px;height:13px;margin-right:5px}.legacy{background:#5b7592}.alp{background:#db6737}.numbers{font-size:12px;color:#40536b;font-variant-numeric:tabular-nums}table{border-collapse:collapse;font-size:13px;width:100%;background:white;margin-top:20px}td,th{padding:10px;text-align:right;border-bottom:1px solid #dfe5ed}td:first-child,th:first-child{text-align:left}.scroll{overflow:auto}a{color:#225bb1}small{display:block;color:#596a7d;margin:12px 0}button{padding:7px}
</style><main><h1>ALP: compression speed, decompression speed, and CPU</h1>
<p>Apple M5 Pro · Go 1.27.1 · one Go execution thread · six repetitions. Fixtures are synthetic. Values are medians; full input validation precedes measurement.</p>
<div class="controls">
<label>Build<select id="backend"><option value="simd">SIMD / NEON</option><option value="scalar">Scalar</option></select></label>
<label>Input<select id="family"><option value="float">Floats</option><option value="integer-histogram">Integer histograms</option><option value="float-histogram">Float histograms</option></select></label>
<label>Samples per batch<select id="samples"><option>120</option><option>32</option><option>1024</option></select></label>
<label>Buckets<select id="buckets"></select></label>
<label>Timestamps<select id="timing"><option value="regular">Regular, constant ST</option><option value="jitter">Irregular, constant ST</option><option value="changing-st">Regular, changing ST</option></select></label>
<label>Decode mode<select id="decode"><option value="decode">Warm, native output</option><option value="decode-float">Warm, float histogram output</option><option value="decode-cold">Cold iterator allocation</option></select></label>
<label>Speed or CPU cost<select id="metric"><option value="M-samples/s">Throughput (higher is better)</option><option value="cpu-seconds/M-samples">CPU cost (lower is better)</option></select></label>
</div><div class="legend"><span><i class="swatch legacy"></i>XOR2 / legacy histogram</span><span><i class="swatch alp"></i>ALP / histogram v3</span></div>
<div id="charts"></div><div class="scroll" id="table"></div>
<small>CPU cost = user + system CPU time per million input observations. For histograms, one observation is a whole histogram. CPU utilization is relative to one core; 100% does not mean all 18 cores. Warm decoding reuses iterator/output buffers. Reset cases can produce multiple chunks per batch. Missing combinations were not benchmarked.</small>
<p><a href="report.md">Methodology and findings</a> · <a href="summary.csv">Download all medians (CSV)</a> · <a href="tables.md">All comparisons</a></p>
<script>const records=__DATA__;
const ids=['backend','family','samples','buckets','timing','metric','decode'];const el=Object.fromEntries(ids.map(id=>[id,document.getElementById(id)]));
function bucketOptions(){const previous=el.buckets.value;const options=el.family.value==='float'?[0]:[8,128,1031];el.buckets.innerHTML=options.map(v=>`<option>${v}</option>`).join('');if(previous!==''&&options.includes(Number(previous)))el.buckets.value=previous;}
function fmt(n){return Number(n).toLocaleString(undefined,{maximumFractionDigits:3});}
function render(){
 const selected=records.filter(r=>r.suite==='BenchmarkALPMatrix'&&r.backend===el.backend.value&&r.family===el.family.value&&r.samples===Number(el.samples.value)&&r.buckets===Number(el.buckets.value)&&r.timing===el.timing.value&&r.codec!=='ALPv1');
 const patterns=[...new Set(selected.map(r=>r.pattern))].sort();
 const lookup=(p,op,isALP)=>selected.find(r=>r.pattern===p&&r.operation===op&&(['ALP','ALPv3'].includes(r.codec)===isALP));
 const metric=el.metric.value,units=metric==='M-samples/s'?'million observations/s':'CPU seconds / million observations';
 const specs=[['encode','Compression',metric],[el.decode.value,'Decompression',metric],['encode','Encoded size','encoded-B/sample']];
 document.getElementById('charts').innerHTML=specs.map(([op,title,field])=>{const values=patterns.flatMap(p=>[lookup(p,op,false),lookup(p,op,true)]).filter(Boolean);const max=Math.max(1,...values.map(r=>r[field]));return `<section class="chart"><h2>${title}</h2><small>${field==='encoded-B/sample'?'bytes / observation · lower is better':units}</small>${patterns.map(p=>{const a=lookup(p,op,false),b=lookup(p,op,true);if(!a||!b)return '';return `<div class="barrow"><div class="pattern">${p}</div><div class="bar legacy" style="width:${a[field]/max*100}%" title="Legacy: ${fmt(a[field])}"></div><div class="bar alp" style="width:${b[field]/max*100}%" title="ALP: ${fmt(b[field])}"></div><div class="numbers">${fmt(a[field])} → ${fmt(b[field])}</div></div>`}).join('')||'<p>No measured cases for this selection.</p>'}</section>`}).join('');
 document.getElementById('table').innerHTML=`<table><thead><tr><th>Pattern / codec</th><th>Encode M/s</th><th>Decode M/s</th><th>Encode CPU s/M</th><th>Decode CPU s/M</th><th>Encode CPU % core</th><th>Decode CPU % core</th><th>Bytes / sample</th><th>Chunks / batch</th></tr></thead><tbody>${patterns.flatMap(p=>[false,true].map(alp=>{const a=lookup(p,'encode',alp),d=lookup(p,el.decode.value,alp);if(!a||!d)return '';return `<tr><td>${p} / ${a.codec}</td><td>${fmt(a['M-samples/s'])}</td><td>${fmt(d['M-samples/s'])}</td><td>${fmt(a['cpu-seconds/M-samples'])}</td><td>${fmt(d['cpu-seconds/M-samples'])}</td><td>${fmt(a['cpu-%core'])}</td><td>${fmt(d['cpu-%core'])}</td><td>${fmt(a['encoded-B/sample'])}</td><td>${fmt(a['chunks/op'])}</td></tr>`})).join('')}</tbody></table>`;
}
for(const id of ids)el[id].addEventListener('change',()=>{if(id==='family')bucketOptions();render()});bucketOptions();render();
</script></main></html>'''
    (out / 'explorer.html').write_text(template.replace('__DATA__', json.dumps(summary)))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('out', type=Path)
    args = parser.parse_args()
    summary = summarize(args.out)
    comparisons = pairs(summary)
    (args.out / 'comparisons.json').write_text(json.dumps(comparisons, indent=2) + '\n')
    write_benchstat_inputs(args.out)
    markdown_tables(args.out, summary, comparisons)
    explorer(args.out, summary)
    print(f'{len(summary)} medians; {len(comparisons)} codec comparisons.')


if __name__ == '__main__':
    main()
