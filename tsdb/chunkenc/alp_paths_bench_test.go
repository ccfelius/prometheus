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

	"github.com/prometheus/prometheus/tsdb/tsdbutil"
)

func BenchmarkALPHistogramPaths(b *testing.B) {
	for _, enc := range []Encoding{EncALPHistogram, EncALPFloatHistogram} {
		b.Run(enc.String(), func(b *testing.B) {
			hs := tsdbutil.GenerateTestHistograms(120)
			fs := tsdbutil.GenerateTestFloatHistograms(120)
			appendSample := func(a Appender, i int) Appender {
				var err error
				if enc == EncALPHistogram {
					_, _, a, err = a.AppendHistogram(nil, 1, int64(i), hs[i], true)
				} else {
					_, _, a, err = a.AppendFloatHistogram(nil, 1, int64(i), fs[i], true)
				}
				if err != nil {
					b.Fatal(err)
				}
				return a
			}
			legacy := EncHistogramST
			if enc == EncALPFloatHistogram {
				legacy = EncFloatHistogramST
			}
			source, _ := NewEmptyChunk(legacy)
			a, _ := source.Appender()
			for i := range 120 {
				a = appendSample(a, i)
			}
			b.Run("recode", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					c, err := RecodeToALPHistogram(source)
					if err != nil {
						b.Fatal(err)
					}
					alpBenchSink = float64(len(c.Bytes()))
				}
			})
			b.Run("query-after-append", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					c, _ := NewEmptyChunk(enc)
					a, _ := c.Appender()
					var it Iterator
					for i := range 120 {
						a = appendSample(a, i)
						it = c.Iterator(it)
						if it.Next() == ValNone {
							b.Fatal(it.Err())
						}
						alpBenchSink = float64(it.AtT())
					}
				}
			})
		})
	}
}

func BenchmarkALPColdRead(b *testing.B) {
	for _, enc := range []Encoding{EncALP, EncALPHistogram, EncALPFloatHistogram} {
		b.Run(enc.String(), func(b *testing.B) {
			c, _ := NewEmptyChunk(enc)
			a, _ := c.Appender()
			for i := range 120 {
				switch enc {
				case EncALP:
					a.Append(0, int64(i), float64(i)/100)
				case EncALPHistogram:
					_, _, a, _ = a.AppendHistogram(nil, 0, int64(i), tsdbutil.GenerateTestHistogram(int64(i)), true)
				case EncALPFloatHistogram:
					_, _, a, _ = a.AppendFloatHistogram(nil, 0, int64(i), tsdbutil.GenerateTestFloatHistogram(int64(i)), true)
				}
			}
			c.Compact()
			b.ReportAllocs()
			for b.Loop() {
				it := c.Iterator(nil)
				if it.Next() == ValNone {
					b.Fatal(it.Err())
				}
				alpBenchSink = float64(it.AtT())
			}
		})
	}
}
