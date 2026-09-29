// Copyright The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package chunkenc

import (
	"testing"

	"github.com/prometheus/prometheus/model/histogram"
)

// BenchmarkALPHistogramWorkloads includes the integer-to-float materialization
// used by queries, wide histograms, and genuinely fractional floating counts.
func BenchmarkALPHistogramWorkloads(b *testing.B) {
	for _, tc := range []struct {
		name     string
		buckets  int
		floating bool
	}{
		{"integer-8", 8, false}, {"integer-128", 128, false}, {"integer-1031", 1031, false}, {"fractional-128", 128, true},
	} {
		enc := EncHistogramST
		if tc.floating {
			enc = EncFloatHistogramST
		}
		source, _ := NewEmptyChunk(enc)
		a, _ := source.Appender()
		for i := range 120 {
			h := &histogram.Histogram{Schema: 1, Count: uint64(tc.buckets * (i + 1)), Sum: float64(i+1) * 18.4, PositiveSpans: []histogram.Span{{Length: uint32(tc.buckets)}}, PositiveBuckets: make([]int64, tc.buckets)}
			h.PositiveBuckets[0] = int64(i + 1)
			var err error
			if tc.floating {
				fh := h.ToFloat(nil)
				fh.Count = 0
				for j := range fh.PositiveBuckets {
					fh.PositiveBuckets[j] += float64(j%7) / 100
					fh.Count += fh.PositiveBuckets[j]
				}
				_, _, a, err = a.AppendFloatHistogram(nil, 1, int64(i*15000), fh, true)
			} else {
				_, _, a, err = a.AppendHistogram(nil, 1, int64(i*15000), h, true)
			}
			if err != nil {
				b.Fatal(err)
			}
		}
		var e ALPEncoder
		c, err := e.RecodeHistogramV3(source)
		if err != nil {
			b.Fatal(err)
		}
		for _, encoded := range []Chunk{source, c} {
			b.Run(tc.name+"/"+encoded.Encoding().String()+"/read-float", func(b *testing.B) {
				var it Iterator
				var h *histogram.FloatHistogram
				b.ReportAllocs()
				for b.Loop() {
					it = encoded.Iterator(it)
					for it.Next() != ValNone {
						_, h = it.AtFloatHistogram(h)
					}
					if it.Err() != nil {
						b.Fatal(it.Err())
					}
				}
				b.ReportMetric(float64(len(encoded.Bytes()))/120, "encoded-B/sample")
			})
		}
	}
}
