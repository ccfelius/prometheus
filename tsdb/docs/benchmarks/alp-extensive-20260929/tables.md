# Detailed benchmark medians

All rates are per input observation: one float or one whole histogram. CPU cost is process user + system CPU seconds per million observations. Decode materializes values; integer `decode-float` produces float histograms. Six repetitions per row.

## float: SIMD, 120 samples, regular timestamps

| Pattern | Buckets | Bytes/sample legacy → ALP | Encode M samples/s legacy → ALP | Decode M samples/s legacy → ALP | Encode CPU s/M legacy → ALP | Decode CPU s/M legacy → ALP |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| computed | 0 | 7.142 → 6.808 | 58.852 → 12.568 | 128.404 → 210.748 | 0.0168 → 0.0788 | 0.0078 → 0.0047 |
| constant | 0 | 0.308 → 0.400 | 188.044 → 87.178 | 313.275 → 425.306 | 0.0052 → 0.0110 | 0.0032 → 0.0023 |
| counter | 0 | 2.000 → 1.842 | 100.671 → 43.415 | 166.216 → 321.586 | 0.0099 → 0.0225 | 0.0060 → 0.0031 |
| decimal2 | 0 | 5.800 → 1.375 | 62.794 → 41.566 | 127.734 → 304.414 | 0.0158 → 0.0236 | 0.0078 → 0.0033 |
| decimal6 | 0 | 5.767 → 1.458 | 62.321 → 16.368 | 128.466 → 308.127 | 0.0159 → 0.0604 | 0.0078 → 0.0032 |
| noisy-decimal | 0 | 7.150 → 2.242 | 59.012 → 40.363 | 127.402 → 293.758 | 0.0168 → 0.0243 | 0.0078 → 0.0034 |
| outliers20 | 0 | 7.475 → 5.042 | 57.803 → 16.719 | 126.229 → 306.122 | 0.0171 → 0.0594 | 0.0079 → 0.0033 |
| random-bits | 0 | 8.517 → 8.333 | 52.049 → 9.608 | 123.153 → 380.711 | 0.0190 → 0.1034 | 0.0081 → 0.0026 |
| random-finite | 0 | 8.500 → 7.542 | 52.655 → 10.837 | 123.922 → 210.637 | 0.0187 → 0.0917 | 0.0081 → 0.0047 |
| stale5 | 0 | 5.725 → 1.875 | 65.288 → 41.408 | 133.341 → 306.631 | 0.0151 → 0.0236 | 0.0075 → 0.0033 |

## integer-histogram: SIMD, 120 samples, regular timestamps

| Pattern | Buckets | Bytes/sample legacy → ALP | Encode M samples/s legacy → ALP | Decode M samples/s legacy → ALP | Encode CPU s/M legacy → ALP | Decode CPU s/M legacy → ALP |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| bursty | 8 | 14.060 → 20.360 | 5.075 → 2.365 | 13.810 → 27.555 | 0.1963 → 0.4216 | 0.0724 → 0.0363 |
| bursty | 128 | 94.920 → 131.300 | 0.491 → 0.246 | 1.210 → 3.939 | 2.0305 → 4.0200 | 0.8250 → 0.2515 |
| gauge | 8 | 33.390 → 26.140 | 3.667 → 1.782 | 7.698 → 26.429 | 0.2716 → 0.5583 | 0.1298 → 0.0378 |
| gauge | 128 | 375.400 → 298.400 | 0.319 → 0.175 | 0.589 → 3.834 | 3.1160 → 5.6960 | 1.6970 → 0.2609 |
| layout | 8 | 9.400 → 7.567 | 6.740 → 2.685 | 20.294 → 26.861 | 0.1482 → 0.3720 | 0.0493 → 0.0372 |
| layout | 128 | 27.710 → 26.030 | 0.887 → 0.503 | 2.574 → 4.448 | 1.1270 → 1.9870 | 0.3886 → 0.2248 |
| resets | 8 | 9.775 → 10.900 | 6.962 → 2.093 | 20.862 → 26.287 | 0.1434 → 0.4753 | 0.0479 → 0.0381 |
| resets | 128 | 29.440 → 33.300 | 0.894 → 0.454 | 2.467 → 4.330 | 1.1170 → 2.1960 | 0.4047 → 0.2294 |
| smooth | 8 | 8.917 → 5.825 | 7.962 → 3.509 | 23.128 → 29.049 | 0.1255 → 0.2848 | 0.0432 → 0.0344 |
| smooth | 128 | 25.070 → 21.230 | 0.942 → 0.519 | 2.629 → 4.522 | 1.0585 → 1.9200 | 0.3796 → 0.2211 |
| smooth | 1031 | 146.600 → 129.600 | 0.129 → 0.074 | 0.352 → 0.627 | 7.7290 → 13.6000 | 2.8440 → 1.5940 |
| stale | 8 | 11.050 → 20.020 | 6.497 → 1.626 | 15.681 → 18.727 | 0.1534 → 0.6124 | 0.0636 → 0.0533 |
| stale | 128 | 36.730 → 62.020 | 0.888 → 0.422 | 2.273 → 3.903 | 1.1240 → 2.3575 | 0.4387 → 0.2556 |

## float-histogram: SIMD, 120 samples, regular timestamps

| Pattern | Buckets | Bytes/sample legacy → ALP | Encode M samples/s legacy → ALP | Decode M samples/s legacy → ALP | Encode CPU s/M legacy → ALP | Decode CPU s/M legacy → ALP |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| bursty | 8 | 20.660 → 22.760 | 4.418 → 2.232 | 15.843 → 41.082 | 0.2257 → 0.4462 | 0.0631 → 0.0243 |
| bursty | 128 | 169.200 → 230.200 | 0.448 → 0.243 | 1.539 → 5.669 | 2.2255 → 4.0965 | 0.6499 → 0.1764 |
| fractional | 8 | 79.880 → 32.190 | 2.983 → 1.363 | 10.772 → 39.519 | 0.3320 → 0.7280 | 0.0927 → 0.0252 |
| fractional | 128 | 943.000 → 225.100 | 0.269 → 0.150 | 0.944 → 5.697 | 3.6700 → 6.5960 | 1.0580 → 0.1742 |
| gauge | 8 | 30.620 → 23.960 | 4.363 → 2.198 | 13.293 → 43.376 | 0.2278 → 0.4521 | 0.0751 → 0.0230 |
| gauge | 128 | 323.600 → 249.400 | 0.435 → 0.236 | 1.171 → 5.221 | 2.2815 → 4.2080 | 0.8525 → 0.1913 |
| layout | 8 | 22.520 → 20.470 | 3.747 → 1.798 | 12.165 → 38.917 | 0.2657 → 0.5543 | 0.0820 → 0.0257 |
| layout | 128 | 186.300 → 253.000 | 0.423 → 0.247 | 1.200 → 7.338 | 2.3580 → 4.0385 | 0.8331 → 0.1363 |
| noisy-fractional | 8 | 79.340 → 73.670 | 3.429 → 1.165 | 10.946 → 27.171 | 0.2888 → 0.8530 | 0.0914 → 0.0368 |
| noisy-fractional | 128 | 940.900 → 832.600 | 0.280 → 0.107 | 0.949 → 3.305 | 3.5315 → 9.2120 | 1.0525 → 0.3023 |
| resets | 8 | 24.010 → 18.770 | 3.658 → 1.478 | 12.891 → 38.406 | 0.2724 → 0.6744 | 0.0774 → 0.0260 |
| resets | 128 | 213.700 → 222.700 | 0.374 → 0.224 | 1.156 → 7.104 | 2.6400 → 4.4345 | 0.8629 → 0.1406 |
| smooth | 8 | 20.690 → 19.020 | 4.266 → 2.174 | 13.317 → 39.364 | 0.2337 → 0.4582 | 0.0750 → 0.0254 |
| smooth | 128 | 177.200 → 250.100 | 0.429 → 0.251 | 1.196 → 7.500 | 2.3185 → 3.9700 | 0.8351 → 0.1332 |
| smooth | 1031 | 1353.000 → 1296.000 | 0.057 → 0.033 | 0.152 → 0.803 | 17.4705 → 30.1410 | 6.5645 → 1.2450 |
| stale | 8 | 24.800 → 22.450 | 3.870 → 1.298 | 11.157 → 27.015 | 0.2576 → 0.7678 | 0.0894 → 0.0370 |
| stale | 128 | 211.800 → 254.900 | 0.364 → 0.213 | 1.130 → 5.913 | 2.6705 → 4.6455 | 0.8645 → 0.1685 |

## Incremental compaction conversion: SIMD

Source chunks already exist. These measurements exclude initial legacy encoding and reuse one encoder for a repeated series. Adaptive floats include periodic retry hints; adaptive histograms still perform a full trial before the size gate.

| Family / pattern | Buckets | Policy | M samples/s | CPU s/M samples | CPU % of one core | Bytes/sample retained | Accepted % |
| --- | ---: | --- | ---: | ---: | ---: | ---: | ---: |
| float-histogram / fractional | 8 | adaptive | 4.186 | 0.2384 | 99.7 | 23.660 | 100.0 |
| float-histogram / fractional | 8 | forced | 4.194 | 0.2379 | 99.8 | 23.660 | 100.0 |
| float-histogram / noisy-fractional | 128 | adaptive | 0.180 | 5.5025 | 99.0 | 832.600 | 100.0 |
| float-histogram / noisy-fractional | 128 | forced | 0.180 | 5.5090 | 99.0 | 832.600 | 100.0 |
| float / computed | 0 | adaptive | 1021.277 | 0.0010 | 99.8 | 7.142 | 0.0 |
| float / computed | 0 | forced | 12.819 | 0.0779 | 99.9 | 6.808 | 100.0 |
| float / decimal2 | 0 | adaptive | 48.212 | 0.0207 | 99.7 | 1.375 | 100.0 |
| float / decimal2 | 0 | forced | 49.271 | 0.0202 | 99.6 | 1.375 | 100.0 |
| float / random-bits | 0 | adaptive | 1804.104 | 0.0006 | 99.6 | 8.517 | 0.0 |
| float / random-bits | 0 | forced | 9.779 | 0.1022 | 99.8 | 8.333 | 100.0 |
| integer-histogram / bursty | 128 | adaptive | 0.640 | 1.5590 | 99.9 | 94.920 | 0.0 |
| integer-histogram / bursty | 128 | forced | 0.638 | 1.5630 | 99.8 | 131.300 | 100.0 |
| integer-histogram / smooth | 8 | adaptive | 6.581 | 0.1516 | 99.8 | 5.825 | 100.0 |
| integer-histogram / smooth | 8 | forced | 6.677 | 0.1495 | 99.8 | 5.825 | 100.0 |

## All codec comparisons

Speedup above 1 means ALP processes more observations per second. CPU and size ratios below 1 favor ALP. These are ratios of medians, not aggregate production workload results.

| Build | Family | Pattern | Samples | Buckets | Time | Operation | ALP speedup | ALP CPU / legacy CPU | ALP bytes / legacy bytes |
| --- | --- | --- | ---: | ---: | --- | --- | ---: | ---: | ---: |
| scalar | float | computed | 32 | 0 | regular | decode | 1.321× | 0.756× | 1.100× |
| scalar | float | computed | 32 | 0 | regular | encode | 0.086× | 11.634× | 1.100× |
| scalar | float | computed | 120 | 0 | changing-st | decode | 3.145× | 0.328× | 0.825× |
| scalar | float | computed | 120 | 0 | changing-st | encode | 0.251× | 4.017× | 0.825× |
| scalar | float | computed | 120 | 0 | jitter | decode | 1.508× | 0.664× | 0.969× |
| scalar | float | computed | 120 | 0 | jitter | encode | 0.222× | 4.526× | 0.969× |
| scalar | float | computed | 120 | 0 | regular | decode | 1.624× | 0.616× | 0.953× |
| scalar | float | computed | 120 | 0 | regular | encode | 0.195× | 5.145× | 0.953× |
| scalar | float | computed | 1024 | 0 | regular | decode | 3.821× | 0.261× | 0.698× |
| scalar | float | computed | 1024 | 0 | regular | encode | 0.235× | 4.321× | 0.698× |
| scalar | float | constant | 32 | 0 | regular | decode | 1.796× | 0.557× | 1.846× |
| scalar | float | constant | 32 | 0 | regular | encode | 0.567× | 1.731× | 1.846× |
| scalar | float | constant | 120 | 0 | regular | decode | 1.330× | 0.744× | 1.297× |
| scalar | float | constant | 120 | 0 | regular | encode | 0.434× | 2.213× | 1.297× |
| scalar | float | constant | 1024 | 0 | regular | decode | 6.108× | 0.164× | 0.128× |
| scalar | float | constant | 1024 | 0 | regular | encode | 1.541× | 0.647× | 0.128× |
| scalar | float | counter | 32 | 0 | regular | decode | 1.577× | 0.634× | 1.346× |
| scalar | float | counter | 32 | 0 | regular | encode | 0.259× | 3.863× | 1.346× |
| scalar | float | counter | 120 | 0 | regular | decode | 1.712× | 0.584× | 0.921× |
| scalar | float | counter | 120 | 0 | regular | encode | 0.424× | 2.327× | 0.921× |
| scalar | float | counter | 1024 | 0 | regular | decode | 4.469× | 0.224× | 0.377× |
| scalar | float | counter | 1024 | 0 | regular | encode | 0.946× | 1.058× | 0.377× |
| scalar | float | decimal2 | 32 | 0 | regular | decode | 1.651× | 0.607× | 0.429× |
| scalar | float | decimal2 | 32 | 0 | regular | encode | 0.325× | 3.077× | 0.429× |
| scalar | float | decimal2 | 120 | 0 | changing-st | decode | 3.243× | 0.308× | 0.299× |
| scalar | float | decimal2 | 120 | 0 | changing-st | encode | 0.788× | 1.259× | 0.299× |
| scalar | float | decimal2 | 120 | 0 | jitter | decode | 1.563× | 0.638× | 0.427× |
| scalar | float | decimal2 | 120 | 0 | jitter | encode | 0.688× | 1.446× | 0.427× |
| scalar | float | decimal2 | 120 | 0 | regular | decode | 2.190× | 0.456× | 0.237× |
| scalar | float | decimal2 | 120 | 0 | regular | decode-cold | 1.682× | 0.582× | 0.237× |
| scalar | float | decimal2 | 120 | 0 | regular | encode | 0.642× | 1.543× | 0.237× |
| scalar | float | decimal2 | 1024 | 0 | regular | decode | 5.494× | 0.182× | 0.153× |
| scalar | float | decimal2 | 1024 | 0 | regular | encode | 1.278× | 0.789× | 0.153× |
| scalar | float | decimal6 | 32 | 0 | regular | decode | 1.701× | 0.588× | 0.427× |
| scalar | float | decimal6 | 32 | 0 | regular | encode | 0.096× | 10.538× | 0.427× |
| scalar | float | decimal6 | 120 | 0 | regular | decode | 2.232× | 0.448× | 0.253× |
| scalar | float | decimal6 | 120 | 0 | regular | encode | 0.257× | 3.890× | 0.253× |
| scalar | float | decimal6 | 1024 | 0 | regular | decode | 3.758× | 0.266× | 0.164× |
| scalar | float | decimal6 | 1024 | 0 | regular | encode | 0.981× | 1.030× | 0.164× |
| scalar | float | noisy-decimal | 32 | 0 | regular | decode | 1.748× | 0.572× | 0.483× |
| scalar | float | noisy-decimal | 32 | 0 | regular | encode | 0.351× | 2.854× | 0.483× |
| scalar | float | noisy-decimal | 120 | 0 | regular | decode | 2.153× | 0.465× | 0.314× |
| scalar | float | noisy-decimal | 120 | 0 | regular | encode | 0.666× | 1.484× | 0.314× |
| scalar | float | noisy-decimal | 1024 | 0 | regular | decode | 3.737× | 0.268× | 0.222× |
| scalar | float | noisy-decimal | 1024 | 0 | regular | encode | 1.268× | 0.794× | 0.222× |
| scalar | float | outliers20 | 32 | 0 | regular | decode | 1.716× | 0.583× | 0.625× |
| scalar | float | outliers20 | 32 | 0 | regular | encode | 0.099× | 10.161× | 0.625× |
| scalar | float | outliers20 | 120 | 0 | regular | decode | 2.156× | 0.464× | 0.675× |
| scalar | float | outliers20 | 120 | 0 | regular | encode | 0.262× | 3.833× | 0.675× |
| scalar | float | outliers20 | 1024 | 0 | regular | decode | 5.254× | 0.191× | 0.326× |
| scalar | float | outliers20 | 1024 | 0 | regular | encode | 0.310× | 3.268× | 0.326× |
| scalar | float | random-bits | 32 | 0 | regular | decode | 2.868× | 0.349× | 1.039× |
| scalar | float | random-bits | 32 | 0 | regular | encode | 0.097× | 10.370× | 1.039× |
| scalar | float | random-bits | 120 | 0 | regular | decode | 3.147× | 0.318× | 0.978× |
| scalar | float | random-bits | 120 | 0 | regular | decode-cold | 2.410× | 0.406× | 0.978× |
| scalar | float | random-bits | 120 | 0 | regular | encode | 0.182× | 5.545× | 0.978× |
| scalar | float | random-bits | 1024 | 0 | regular | decode | 8.826× | 0.118× | 0.752× |
| scalar | float | random-bits | 1024 | 0 | regular | encode | 0.220× | 4.596× | 0.752× |
| scalar | float | random-finite | 32 | 0 | regular | decode | 1.309× | 0.764× | 1.021× |
| scalar | float | random-finite | 32 | 0 | regular | encode | 0.093× | 10.914× | 1.021× |
| scalar | float | random-finite | 120 | 0 | regular | decode | 1.709× | 0.585× | 0.887× |
| scalar | float | random-finite | 120 | 0 | regular | encode | 0.182× | 5.531× | 0.887× |
| scalar | float | random-finite | 1024 | 0 | regular | decode | 4.228× | 0.237× | 0.677× |
| scalar | float | random-finite | 1024 | 0 | regular | encode | 0.233× | 4.348× | 0.677× |
| scalar | float | stale5 | 32 | 0 | regular | decode | 1.643× | 0.608× | 0.502× |
| scalar | float | stale5 | 32 | 0 | regular | encode | 0.330× | 3.031× | 0.502× |
| scalar | float | stale5 | 120 | 0 | regular | decode | 2.101× | 0.475× | 0.328× |
| scalar | float | stale5 | 120 | 0 | regular | encode | 0.611× | 1.612× | 0.328× |
| scalar | float | stale5 | 1024 | 0 | regular | decode | 5.489× | 0.182× | 0.220× |
| scalar | float | stale5 | 1024 | 0 | regular | encode | 0.950× | 1.061× | 0.220× |
| scalar | float-histogram | bursty | 32 | 8 | regular | decode | 2.047× | 0.489× | 1.080× |
| scalar | float-histogram | bursty | 32 | 8 | regular | encode | 0.385× | 2.596× | 1.080× |
| scalar | float-histogram | bursty | 120 | 8 | regular | decode | 2.176× | 0.458× | 1.102× |
| scalar | float-histogram | bursty | 120 | 8 | regular | encode | 0.481× | 2.082× | 1.102× |
| scalar | float-histogram | bursty | 120 | 128 | regular | decode | 3.403× | 0.294× | 1.361× |
| scalar | float-histogram | bursty | 120 | 128 | regular | encode | 0.472× | 2.119× | 1.361× |
| scalar | float-histogram | bursty | 1024 | 8 | regular | decode | 2.813× | 0.356× | 1.021× |
| scalar | float-histogram | bursty | 1024 | 8 | regular | encode | 0.378× | 2.649× | 1.021× |
| scalar | float-histogram | fractional | 120 | 8 | regular | decode | 3.140× | 0.319× | 0.403× |
| scalar | float-histogram | fractional | 120 | 8 | regular | encode | 0.379× | 2.649× | 0.403× |
| scalar | float-histogram | fractional | 120 | 128 | regular | decode | 5.416× | 0.185× | 0.239× |
| scalar | float-histogram | fractional | 120 | 128 | regular | encode | 0.526× | 1.907× | 0.239× |
| scalar | float-histogram | gauge | 120 | 8 | regular | decode | 2.656× | 0.376× | 0.782× |
| scalar | float-histogram | gauge | 120 | 8 | regular | encode | 0.480× | 2.087× | 0.782× |
| scalar | float-histogram | gauge | 120 | 128 | regular | decode | 4.116× | 0.243× | 0.771× |
| scalar | float-histogram | gauge | 120 | 128 | regular | encode | 0.510× | 1.958× | 0.771× |
| scalar | float-histogram | layout | 120 | 8 | regular | decode | 2.613× | 0.383× | 0.909× |
| scalar | float-histogram | layout | 120 | 8 | regular | encode | 0.454× | 2.198× | 0.909× |
| scalar | float-histogram | layout | 120 | 128 | regular | decode | 4.238× | 0.236× | 1.358× |
| scalar | float-histogram | layout | 120 | 128 | regular | encode | 0.556× | 1.797× | 1.358× |
| scalar | float-histogram | noisy-fractional | 120 | 8 | regular | decode | 2.489× | 0.402× | 0.929× |
| scalar | float-histogram | noisy-fractional | 120 | 8 | regular | encode | 0.281× | 3.573× | 0.929× |
| scalar | float-histogram | noisy-fractional | 120 | 128 | regular | decode | 3.504× | 0.285× | 0.885× |
| scalar | float-histogram | noisy-fractional | 120 | 128 | regular | encode | 0.291× | 3.448× | 0.885× |
| scalar | float-histogram | resets | 120 | 8 | regular | decode | 2.381× | 0.419× | 0.782× |
| scalar | float-histogram | resets | 120 | 8 | regular | encode | 0.398× | 2.534× | 0.782× |
| scalar | float-histogram | resets | 120 | 128 | regular | decode | 4.225× | 0.237× | 1.042× |
| scalar | float-histogram | resets | 120 | 128 | regular | encode | 0.567× | 1.763× | 1.042× |
| scalar | float-histogram | smooth | 32 | 8 | regular | decode | 2.449× | 0.408× | 0.767× |
| scalar | float-histogram | smooth | 32 | 8 | regular | encode | 0.399× | 2.506× | 0.767× |
| scalar | float-histogram | smooth | 120 | 8 | regular | decode | 2.493× | 0.398× | 0.919× |
| scalar | float-histogram | smooth | 120 | 8 | regular | decode-cold | 1.464× | 0.666× | 0.919× |
| scalar | float-histogram | smooth | 120 | 8 | regular | encode | 0.470× | 2.117× | 0.919× |
| scalar | float-histogram | smooth | 120 | 128 | regular | decode | 4.236× | 0.236× | 1.411× |
| scalar | float-histogram | smooth | 120 | 128 | regular | encode | 0.561× | 1.778× | 1.411× |
| scalar | float-histogram | smooth | 120 | 1031 | regular | decode | 4.580× | 0.218× | 0.958× |
| scalar | float-histogram | smooth | 120 | 1031 | regular | encode | 0.558× | 1.798× | 0.958× |
| scalar | float-histogram | smooth | 1024 | 8 | regular | decode | 2.892× | 0.346× | 0.882× |
| scalar | float-histogram | smooth | 1024 | 8 | regular | encode | 0.499× | 2.001× | 0.882× |
| scalar | float-histogram | stale | 120 | 8 | regular | decode | 1.958× | 0.511× | 0.905× |
| scalar | float-histogram | stale | 120 | 8 | regular | encode | 0.326× | 3.066× | 0.905× |
| scalar | float-histogram | stale | 120 | 128 | regular | decode | 3.646× | 0.274× | 1.203× |
| scalar | float-histogram | stale | 120 | 128 | regular | encode | 0.530× | 1.884× | 1.203× |
| scalar | integer-histogram | bursty | 32 | 8 | regular | decode | 1.619× | 0.617× | 1.282× |
| scalar | integer-histogram | bursty | 32 | 8 | regular | decode-float | 1.645× | 0.608× | 1.282× |
| scalar | integer-histogram | bursty | 32 | 8 | regular | encode | 0.348× | 2.867× | 1.282× |
| scalar | integer-histogram | bursty | 120 | 8 | regular | decode | 1.837× | 0.544× | 1.448× |
| scalar | integer-histogram | bursty | 120 | 8 | regular | decode-float | 1.875× | 0.534× | 1.448× |
| scalar | integer-histogram | bursty | 120 | 8 | regular | encode | 0.454× | 2.198× | 1.448× |
| scalar | integer-histogram | bursty | 120 | 128 | regular | decode | 2.844× | 0.352× | 1.383× |
| scalar | integer-histogram | bursty | 120 | 128 | regular | decode-float | 2.850× | 0.351× | 1.383× |
| scalar | integer-histogram | bursty | 120 | 128 | regular | encode | 0.585× | 1.737× | 1.383× |
| scalar | integer-histogram | bursty | 1024 | 8 | regular | decode | 2.298× | 0.435× | 1.187× |
| scalar | integer-histogram | bursty | 1024 | 8 | regular | decode-float | 2.278× | 0.439× | 1.187× |
| scalar | integer-histogram | bursty | 1024 | 8 | regular | encode | 0.482× | 2.077× | 1.187× |
| scalar | integer-histogram | gauge | 120 | 8 | regular | decode | 3.198× | 0.313× | 0.783× |
| scalar | integer-histogram | gauge | 120 | 8 | regular | decode-float | 3.177× | 0.315× | 0.783× |
| scalar | integer-histogram | gauge | 120 | 8 | regular | encode | 0.479× | 2.087× | 0.783× |
| scalar | integer-histogram | gauge | 120 | 128 | regular | decode | 5.408× | 0.185× | 0.795× |
| scalar | integer-histogram | gauge | 120 | 128 | regular | decode-float | 5.273× | 0.189× | 0.795× |
| scalar | integer-histogram | gauge | 120 | 128 | regular | encode | 0.548× | 1.824× | 0.795× |
| scalar | integer-histogram | layout | 120 | 8 | regular | decode | 1.237× | 0.808× | 0.805× |
| scalar | integer-histogram | layout | 120 | 8 | regular | decode-float | 1.269× | 0.788× | 0.805× |
| scalar | integer-histogram | layout | 120 | 8 | regular | encode | 0.389× | 2.570× | 0.805× |
| scalar | integer-histogram | layout | 120 | 128 | regular | decode | 1.380× | 0.725× | 0.939× |
| scalar | integer-histogram | layout | 120 | 128 | regular | decode-float | 1.491× | 0.671× | 0.939× |
| scalar | integer-histogram | layout | 120 | 128 | regular | encode | 0.537× | 1.858× | 0.939× |
| scalar | integer-histogram | resets | 120 | 8 | regular | decode | 1.203× | 0.831× | 1.115× |
| scalar | integer-histogram | resets | 120 | 8 | regular | decode-float | 1.215× | 0.823× | 1.115× |
| scalar | integer-histogram | resets | 120 | 8 | regular | encode | 0.299× | 3.344× | 1.115× |
| scalar | integer-histogram | resets | 120 | 128 | regular | decode | 1.496× | 0.669× | 1.131× |
| scalar | integer-histogram | resets | 120 | 128 | regular | decode-float | 1.576× | 0.636× | 1.131× |
| scalar | integer-histogram | resets | 120 | 128 | regular | encode | 0.514× | 1.944× | 1.131× |
| scalar | integer-histogram | smooth | 32 | 8 | regular | decode | 1.185× | 0.845× | 1.064× |
| scalar | integer-histogram | smooth | 32 | 8 | regular | decode-float | 1.202× | 0.832× | 1.064× |
| scalar | integer-histogram | smooth | 32 | 8 | regular | encode | 0.308× | 3.251× | 1.064× |
| scalar | integer-histogram | smooth | 120 | 8 | regular | decode | 1.165× | 0.858× | 0.653× |
| scalar | integer-histogram | smooth | 120 | 8 | regular | decode-cold | 0.900× | 1.108× | 0.653× |
| scalar | integer-histogram | smooth | 120 | 8 | regular | decode-float | 1.184× | 0.845× | 0.653× |
| scalar | integer-histogram | smooth | 120 | 8 | regular | encode | 0.425× | 2.349× | 0.653× |
| scalar | integer-histogram | smooth | 120 | 128 | regular | decode | 1.435× | 0.697× | 0.847× |
| scalar | integer-histogram | smooth | 120 | 128 | regular | decode-float | 1.506× | 0.663× | 0.847× |
| scalar | integer-histogram | smooth | 120 | 128 | regular | encode | 0.556× | 1.799× | 0.847× |
| scalar | integer-histogram | smooth | 120 | 1031 | regular | decode | 1.454× | 0.688× | 0.884× |
| scalar | integer-histogram | smooth | 120 | 1031 | regular | decode-float | 1.517× | 0.659× | 0.884× |
| scalar | integer-histogram | smooth | 120 | 1031 | regular | encode | 0.575× | 1.741× | 0.884× |
| scalar | integer-histogram | smooth | 1024 | 8 | regular | decode | 1.448× | 0.691× | 0.421× |
| scalar | integer-histogram | smooth | 1024 | 8 | regular | decode-float | 1.484× | 0.674× | 0.421× |
| scalar | integer-histogram | smooth | 1024 | 8 | regular | encode | 0.464× | 2.156× | 0.421× |
| scalar | integer-histogram | stale | 120 | 8 | regular | decode | 1.168× | 0.855× | 1.812× |
| scalar | integer-histogram | stale | 120 | 8 | regular | decode-float | 1.241× | 0.806× | 1.812× |
| scalar | integer-histogram | stale | 120 | 8 | regular | encode | 0.246× | 4.065× | 1.812× |
| scalar | integer-histogram | stale | 120 | 128 | regular | decode | 1.422× | 0.702× | 1.689× |
| scalar | integer-histogram | stale | 120 | 128 | regular | decode-float | 1.501× | 0.666× | 1.689× |
| scalar | integer-histogram | stale | 120 | 128 | regular | encode | 0.462× | 2.142× | 1.689× |
| simd | float | computed | 32 | 0 | regular | decode | 1.293× | 0.773× | 1.100× |
| simd | float | computed | 32 | 0 | regular | encode | 0.088× | 11.375× | 1.100× |
| simd | float | computed | 120 | 0 | changing-st | decode | 2.849× | 0.351× | 0.825× |
| simd | float | computed | 120 | 0 | changing-st | encode | 0.266× | 3.772× | 0.825× |
| simd | float | computed | 120 | 0 | jitter | decode | 1.466× | 0.682× | 0.969× |
| simd | float | computed | 120 | 0 | jitter | encode | 0.238× | 4.230× | 0.969× |
| simd | float | computed | 120 | 0 | regular | decode | 1.641× | 0.610× | 0.953× |
| simd | float | computed | 120 | 0 | regular | encode | 0.214× | 4.688× | 0.953× |
| simd | float | computed | 1024 | 0 | regular | decode | 4.019× | 0.249× | 0.698× |
| simd | float | computed | 1024 | 0 | regular | encode | 0.290× | 3.499× | 0.698× |
| simd | float | constant | 32 | 0 | regular | decode | 1.666× | 0.609× | 1.846× |
| simd | float | constant | 32 | 0 | regular | encode | 0.532× | 1.842× | 1.846× |
| simd | float | constant | 120 | 0 | regular | decode | 1.358× | 0.731× | 1.297× |
| simd | float | constant | 120 | 0 | regular | encode | 0.464× | 2.099× | 1.297× |
| simd | float | constant | 1024 | 0 | regular | decode | 6.143× | 0.163× | 0.128× |
| simd | float | constant | 1024 | 0 | regular | encode | 1.483× | 0.672× | 0.128× |
| simd | float | counter | 32 | 0 | regular | decode | 1.695× | 0.590× | 1.346× |
| simd | float | counter | 32 | 0 | regular | encode | 0.242× | 4.125× | 1.346× |
| simd | float | counter | 120 | 0 | regular | decode | 1.935× | 0.517× | 0.921× |
| simd | float | counter | 120 | 0 | regular | encode | 0.431× | 2.280× | 0.921× |
| simd | float | counter | 1024 | 0 | regular | decode | 5.165× | 0.194× | 0.377× |
| simd | float | counter | 1024 | 0 | regular | encode | 1.023× | 0.981× | 0.377× |
| simd | float | decimal2 | 32 | 0 | regular | decode | 1.723× | 0.581× | 0.429× |
| simd | float | decimal2 | 32 | 0 | regular | encode | 0.342× | 2.947× | 0.429× |
| simd | float | decimal2 | 120 | 0 | changing-st | decode | 3.501× | 0.288× | 0.299× |
| simd | float | decimal2 | 120 | 0 | changing-st | encode | 0.769× | 1.293× | 0.299× |
| simd | float | decimal2 | 120 | 0 | jitter | decode | 1.738× | 0.577× | 0.427× |
| simd | float | decimal2 | 120 | 0 | jitter | encode | 0.708× | 1.404× | 0.427× |
| simd | float | decimal2 | 120 | 0 | regular | decode | 2.383× | 0.420× | 0.237× |
| simd | float | decimal2 | 120 | 0 | regular | decode-cold | 1.837× | 0.535× | 0.237× |
| simd | float | decimal2 | 120 | 0 | regular | encode | 0.662× | 1.494× | 0.237× |
| simd | float | decimal2 | 1024 | 0 | regular | decode | 5.946× | 0.168× | 0.153× |
| simd | float | decimal2 | 1024 | 0 | regular | encode | 1.368× | 0.739× | 0.153× |
| simd | float | decimal6 | 32 | 0 | regular | decode | 1.703× | 0.587× | 0.427× |
| simd | float | decimal6 | 32 | 0 | regular | encode | 0.096× | 10.447× | 0.427× |
| simd | float | decimal6 | 120 | 0 | regular | decode | 2.399× | 0.417× | 0.253× |
| simd | float | decimal6 | 120 | 0 | regular | encode | 0.263× | 3.805× | 0.253× |
| simd | float | decimal6 | 1024 | 0 | regular | decode | 5.911× | 0.169× | 0.164× |
| simd | float | decimal6 | 1024 | 0 | regular | encode | 1.062× | 0.951× | 0.164× |
| simd | float | noisy-decimal | 32 | 0 | regular | decode | 1.907× | 0.525× | 0.483× |
| simd | float | noisy-decimal | 32 | 0 | regular | encode | 0.357× | 2.801× | 0.483× |
| simd | float | noisy-decimal | 120 | 0 | regular | decode | 2.306× | 0.435× | 0.314× |
| simd | float | noisy-decimal | 120 | 0 | regular | encode | 0.684× | 1.450× | 0.314× |
| simd | float | noisy-decimal | 1024 | 0 | regular | decode | 4.096× | 0.244× | 0.222× |
| simd | float | noisy-decimal | 1024 | 0 | regular | encode | 1.352× | 0.746× | 0.222× |
| simd | float | outliers20 | 32 | 0 | regular | decode | 1.734× | 0.576× | 0.625× |
| simd | float | outliers20 | 32 | 0 | regular | encode | 0.104× | 9.731× | 0.625× |
| simd | float | outliers20 | 120 | 0 | regular | decode | 2.425× | 0.412× | 0.675× |
| simd | float | outliers20 | 120 | 0 | regular | encode | 0.289× | 3.473× | 0.675× |
| simd | float | outliers20 | 1024 | 0 | regular | decode | 5.583× | 0.179× | 0.326× |
| simd | float | outliers20 | 1024 | 0 | regular | encode | 0.362× | 2.803× | 0.326× |
| simd | float | random-bits | 32 | 0 | regular | decode | 2.893× | 0.346× | 1.039× |
| simd | float | random-bits | 32 | 0 | regular | encode | 0.099× | 10.217× | 1.039× |
| simd | float | random-bits | 120 | 0 | regular | decode | 3.091× | 0.323× | 0.978× |
| simd | float | random-bits | 120 | 0 | regular | decode-cold | 2.401× | 0.407× | 0.978× |
| simd | float | random-bits | 120 | 0 | regular | encode | 0.185× | 5.448× | 0.978× |
| simd | float | random-bits | 1024 | 0 | regular | decode | 7.735× | 0.129× | 0.752× |
| simd | float | random-bits | 1024 | 0 | regular | encode | 0.230× | 4.406× | 0.752× |
| simd | float | random-finite | 32 | 0 | regular | decode | 1.340× | 0.746× | 1.021× |
| simd | float | random-finite | 32 | 0 | regular | encode | 0.096× | 10.488× | 1.021× |
| simd | float | random-finite | 120 | 0 | regular | decode | 1.700× | 0.587× | 0.887× |
| simd | float | random-finite | 120 | 0 | regular | encode | 0.206× | 4.894× | 0.887× |
| simd | float | random-finite | 1024 | 0 | regular | decode | 4.235× | 0.236× | 0.677× |
| simd | float | random-finite | 1024 | 0 | regular | encode | 0.262× | 3.851× | 0.677× |
| simd | float | stale5 | 32 | 0 | regular | decode | 1.670× | 0.599× | 0.502× |
| simd | float | stale5 | 32 | 0 | regular | encode | 0.339× | 2.950× | 0.502× |
| simd | float | stale5 | 120 | 0 | regular | decode | 2.300× | 0.435× | 0.328× |
| simd | float | stale5 | 120 | 0 | regular | encode | 0.634× | 1.561× | 0.328× |
| simd | float | stale5 | 1024 | 0 | regular | decode | 5.892× | 0.170× | 0.220× |
| simd | float | stale5 | 1024 | 0 | regular | encode | 1.027× | 0.986× | 0.220× |
| simd | float-histogram | bursty | 32 | 8 | regular | decode | 2.499× | 0.400× | 1.080× |
| simd | float-histogram | bursty | 32 | 8 | regular | encode | 0.400× | 2.499× | 1.080× |
| simd | float-histogram | bursty | 120 | 8 | regular | decode | 2.593× | 0.386× | 1.102× |
| simd | float-histogram | bursty | 120 | 8 | regular | encode | 0.505× | 1.978× | 1.102× |
| simd | float-histogram | bursty | 120 | 128 | regular | decode | 3.684× | 0.272× | 1.361× |
| simd | float-histogram | bursty | 120 | 128 | regular | encode | 0.543× | 1.841× | 1.361× |
| simd | float-histogram | bursty | 1024 | 8 | regular | decode | 3.444× | 0.290× | 1.021× |
| simd | float-histogram | bursty | 1024 | 8 | regular | encode | 0.414× | 2.420× | 1.021× |
| simd | float-histogram | fractional | 120 | 8 | regular | decode | 3.669× | 0.272× | 0.403× |
| simd | float-histogram | fractional | 120 | 8 | regular | encode | 0.457× | 2.193× | 0.403× |
| simd | float-histogram | fractional | 120 | 128 | regular | decode | 6.035× | 0.165× | 0.239× |
| simd | float-histogram | fractional | 120 | 128 | regular | encode | 0.557× | 1.797× | 0.239× |
| simd | float-histogram | gauge | 120 | 8 | regular | decode | 3.263× | 0.306× | 0.782× |
| simd | float-histogram | gauge | 120 | 8 | regular | encode | 0.504× | 1.985× | 0.782× |
| simd | float-histogram | gauge | 120 | 128 | regular | decode | 4.460× | 0.224× | 0.771× |
| simd | float-histogram | gauge | 120 | 128 | regular | encode | 0.543× | 1.844× | 0.771× |
| simd | float-histogram | layout | 120 | 8 | regular | decode | 3.199× | 0.313× | 0.909× |
| simd | float-histogram | layout | 120 | 8 | regular | encode | 0.480× | 2.086× | 0.909× |
| simd | float-histogram | layout | 120 | 128 | regular | decode | 6.113× | 0.164× | 1.358× |
| simd | float-histogram | layout | 120 | 128 | regular | encode | 0.583× | 1.713× | 1.358× |
| simd | float-histogram | noisy-fractional | 120 | 8 | regular | decode | 2.482× | 0.403× | 0.929× |
| simd | float-histogram | noisy-fractional | 120 | 8 | regular | encode | 0.340× | 2.953× | 0.929× |
| simd | float-histogram | noisy-fractional | 120 | 128 | regular | decode | 3.484× | 0.287× | 0.885× |
| simd | float-histogram | noisy-fractional | 120 | 128 | regular | encode | 0.382× | 2.609× | 0.885× |
| simd | float-histogram | resets | 120 | 8 | regular | decode | 2.979× | 0.336× | 0.782× |
| simd | float-histogram | resets | 120 | 8 | regular | encode | 0.404× | 2.476× | 0.782× |
| simd | float-histogram | resets | 120 | 128 | regular | decode | 6.143× | 0.163× | 1.042× |
| simd | float-histogram | resets | 120 | 128 | regular | encode | 0.600× | 1.680× | 1.042× |
| simd | float-histogram | smooth | 32 | 8 | regular | decode | 2.919× | 0.343× | 0.767× |
| simd | float-histogram | smooth | 32 | 8 | regular | encode | 0.413× | 2.423× | 0.767× |
| simd | float-histogram | smooth | 120 | 8 | regular | decode | 2.956× | 0.338× | 0.919× |
| simd | float-histogram | smooth | 120 | 8 | regular | decode-cold | 1.626× | 0.604× | 0.919× |
| simd | float-histogram | smooth | 120 | 8 | regular | encode | 0.510× | 1.961× | 0.919× |
| simd | float-histogram | smooth | 120 | 128 | regular | decode | 6.270× | 0.159× | 1.411× |
| simd | float-histogram | smooth | 120 | 128 | regular | encode | 0.584× | 1.712× | 1.411× |
| simd | float-histogram | smooth | 120 | 1031 | regular | decode | 5.279× | 0.190× | 0.958× |
| simd | float-histogram | smooth | 120 | 1031 | regular | encode | 0.580× | 1.725× | 0.958× |
| simd | float-histogram | smooth | 1024 | 8 | regular | decode | 3.559× | 0.282× | 0.882× |
| simd | float-histogram | smooth | 1024 | 8 | regular | encode | 0.532× | 1.884× | 0.882× |
| simd | float-histogram | stale | 120 | 8 | regular | decode | 2.421× | 0.413× | 0.905× |
| simd | float-histogram | stale | 120 | 8 | regular | encode | 0.335× | 2.980× | 0.905× |
| simd | float-histogram | stale | 120 | 128 | regular | decode | 5.232× | 0.195× | 1.203× |
| simd | float-histogram | stale | 120 | 128 | regular | encode | 0.585× | 1.740× | 1.203× |
| simd | integer-histogram | bursty | 32 | 8 | regular | decode | 1.718× | 0.582× | 1.282× |
| simd | integer-histogram | bursty | 32 | 8 | regular | decode-float | 1.787× | 0.560× | 1.282× |
| simd | integer-histogram | bursty | 32 | 8 | regular | encode | 0.354× | 2.827× | 1.282× |
| simd | integer-histogram | bursty | 120 | 8 | regular | decode | 1.995× | 0.501× | 1.448× |
| simd | integer-histogram | bursty | 120 | 8 | regular | decode-float | 2.082× | 0.480× | 1.448× |
| simd | integer-histogram | bursty | 120 | 8 | regular | encode | 0.466× | 2.148× | 1.448× |
| simd | integer-histogram | bursty | 120 | 128 | regular | decode | 3.256× | 0.305× | 1.383× |
| simd | integer-histogram | bursty | 120 | 128 | regular | decode-float | 3.057× | 0.319× | 1.383× |
| simd | integer-histogram | bursty | 120 | 128 | regular | encode | 0.502× | 1.980× | 1.383× |
| simd | integer-histogram | bursty | 1024 | 8 | regular | decode | 2.444× | 0.409× | 1.187× |
| simd | integer-histogram | bursty | 1024 | 8 | regular | decode-float | 2.519× | 0.397× | 1.187× |
| simd | integer-histogram | bursty | 1024 | 8 | regular | encode | 0.487× | 2.051× | 1.187× |
| simd | integer-histogram | gauge | 120 | 8 | regular | decode | 3.433× | 0.291× | 0.783× |
| simd | integer-histogram | gauge | 120 | 8 | regular | decode-float | 3.570× | 0.280× | 0.783× |
| simd | integer-histogram | gauge | 120 | 8 | regular | encode | 0.486× | 2.056× | 0.783× |
| simd | integer-histogram | gauge | 120 | 128 | regular | decode | 6.506× | 0.154× | 0.795× |
| simd | integer-histogram | gauge | 120 | 128 | regular | decode-float | 6.372× | 0.157× | 0.795× |
| simd | integer-histogram | gauge | 120 | 128 | regular | encode | 0.548× | 1.828× | 0.795× |
| simd | integer-histogram | layout | 120 | 8 | regular | decode | 1.324× | 0.756× | 0.805× |
| simd | integer-histogram | layout | 120 | 8 | regular | decode-float | 1.375× | 0.727× | 0.805× |
| simd | integer-histogram | layout | 120 | 8 | regular | encode | 0.398× | 2.510× | 0.805× |
| simd | integer-histogram | layout | 120 | 128 | regular | decode | 1.728× | 0.578× | 0.939× |
| simd | integer-histogram | layout | 120 | 128 | regular | decode-float | 1.839× | 0.544× | 0.939× |
| simd | integer-histogram | layout | 120 | 128 | regular | encode | 0.567× | 1.763× | 0.939× |
| simd | integer-histogram | resets | 120 | 8 | regular | decode | 1.260× | 0.794× | 1.115× |
| simd | integer-histogram | resets | 120 | 8 | regular | decode-float | 1.307× | 0.765× | 1.115× |
| simd | integer-histogram | resets | 120 | 8 | regular | encode | 0.301× | 3.314× | 1.115× |
| simd | integer-histogram | resets | 120 | 128 | regular | decode | 1.755× | 0.567× | 1.131× |
| simd | integer-histogram | resets | 120 | 128 | regular | decode-float | 1.837× | 0.543× | 1.131× |
| simd | integer-histogram | resets | 120 | 128 | regular | encode | 0.508× | 1.966× | 1.131× |
| simd | integer-histogram | smooth | 32 | 8 | regular | decode | 1.238× | 0.807× | 1.064× |
| simd | integer-histogram | smooth | 32 | 8 | regular | decode-float | 1.292× | 0.774× | 1.064× |
| simd | integer-histogram | smooth | 32 | 8 | regular | encode | 0.312× | 3.210× | 1.064× |
| simd | integer-histogram | smooth | 120 | 8 | regular | decode | 1.256× | 0.796× | 0.653× |
| simd | integer-histogram | smooth | 120 | 8 | regular | decode-cold | 0.946× | 1.054× | 0.653× |
| simd | integer-histogram | smooth | 120 | 8 | regular | decode-float | 1.315× | 0.761× | 0.653× |
| simd | integer-histogram | smooth | 120 | 8 | regular | encode | 0.441× | 2.269× | 0.653× |
| simd | integer-histogram | smooth | 120 | 128 | regular | decode | 1.720× | 0.582× | 0.847× |
| simd | integer-histogram | smooth | 120 | 128 | regular | decode-float | 1.789× | 0.559× | 0.847× |
| simd | integer-histogram | smooth | 120 | 128 | regular | encode | 0.551× | 1.814× | 0.847× |
| simd | integer-histogram | smooth | 120 | 1031 | regular | decode | 1.784× | 0.560× | 0.884× |
| simd | integer-histogram | smooth | 120 | 1031 | regular | decode-float | 1.804× | 0.554× | 0.884× |
| simd | integer-histogram | smooth | 120 | 1031 | regular | encode | 0.568× | 1.760× | 0.884× |
| simd | integer-histogram | smooth | 1024 | 8 | regular | decode | 1.527× | 0.655× | 0.421× |
| simd | integer-histogram | smooth | 1024 | 8 | regular | decode-float | 1.596× | 0.627× | 0.421× |
| simd | integer-histogram | smooth | 1024 | 8 | regular | encode | 0.475× | 2.108× | 0.421× |
| simd | integer-histogram | stale | 120 | 8 | regular | decode | 1.194× | 0.838× | 1.812× |
| simd | integer-histogram | stale | 120 | 8 | regular | decode-float | 1.312× | 0.762× | 1.812× |
| simd | integer-histogram | stale | 120 | 8 | regular | encode | 0.250× | 3.993× | 1.812× |
| simd | integer-histogram | stale | 120 | 128 | regular | decode | 1.717× | 0.583× | 1.689× |
| simd | integer-histogram | stale | 120 | 128 | regular | decode-float | 1.787× | 0.559× | 1.689× |
| simd | integer-histogram | stale | 120 | 128 | regular | encode | 0.476× | 2.097× | 1.689× |
