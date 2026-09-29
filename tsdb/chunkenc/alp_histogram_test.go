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
	"math"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/value"
	"github.com/prometheus/prometheus/tsdb/tsdbutil"
)

func TestALPHistograms(t *testing.T) {
	t.Run("resume preserves reset hint and snapshot", func(t *testing.T) {
		for _, floating := range []bool{false, true} {
			legacy := EncHistogramST
			if floating {
				legacy = EncFloatHistogramST
			}
			previous, _ := NewEmptyChunk(legacy)
			prev, _ := previous.Appender()
			if floating {
				_, _, prev, _ = prev.AppendFloatHistogram(nil, 1, 1, tsdbutil.GenerateTestFloatHistogram(1), true)
			} else {
				_, _, prev, _ = prev.AppendHistogram(nil, 1, 1, tsdbutil.GenerateTestHistogram(1), true)
			}
			c, _ := NewEmptyChunk(legacy)
			a, _ := c.Appender()
			var err error
			if floating {
				_, _, _, err = a.AppendFloatHistogram(prev, 1, 2, tsdbutil.GenerateTestFloatHistogram(2), true)
			} else {
				_, _, _, err = a.AppendHistogram(prev, 1, 2, tsdbutil.GenerateTestHistogram(2), true)
			}
			require.NoError(t, err)
			ref := c.Iterator(nil)
			require.NotEqual(t, ValNone, ref.Next())
			_, original := ref.AtFloatHistogram(nil)
			require.Equal(t, histogram.UnknownCounterReset, original.CounterResetHint, "legacy floating=%v", floating)
			c, err = RecodeToALPHistogram(c)
			require.NoError(t, err)
			encoded := c.Bytes()
			require.Equal(t, byte(NotCounterReset), encoded[3])
			saved := slices.Clone(encoded)
			snapshot := c.Iterator(nil)
			a, err = c.Appender()
			require.NoError(t, err)
			if floating {
				_, _, _, err = a.AppendFloatHistogram(nil, 1, 3, tsdbutil.GenerateTestFloatHistogram(3), true)
			} else {
				_, _, _, err = a.AppendHistogram(nil, 1, 3, tsdbutil.GenerateTestHistogram(3), true)
			}
			require.NoError(t, err)
			require.Equal(t, saved, encoded)
			require.Equal(t, byte(NotCounterReset), c.Bytes()[3])
			for i, it := range []Iterator{snapshot, c.Iterator(nil)} {
				require.NotEqual(t, ValNone, it.Next())
				_, h := it.AtFloatHistogram(nil)
				require.Equal(t, histogram.UnknownCounterReset, h.CounterResetHint, "iterator=%d floating=%v", i, floating)
			}
			require.Equal(t, ValNone, snapshot.Next())
			require.NoError(t, snapshot.Err())
			pool := NewPool()
			enc := c.Encoding()
			require.NoError(t, pool.Put(c))
			require.Zero(t, c.NumSamples())
			c, err = pool.Get(enc, saved)
			require.NoError(t, err)
			require.Equal(t, 1, c.NumSamples())
		}
	})
	for _, enc := range []Encoding{EncALPHistogram, EncALPFloatHistogram} {
		for _, pattern := range []string{"counter", "gauge", "custom", "layout", "reset", "large", "stale", "special"} {
			t.Run(enc.String()+"/"+pattern, func(t *testing.T) {
				build := func(enc Encoding) []Chunk {
					c, err := NewEmptyChunk(enc)
					require.NoError(t, err)
					a, err := c.Appender()
					require.NoError(t, err)
					result := []Chunk{c}
					for i := range 129 {
						h := tsdbutil.GenerateTestHistogram(int64(i))
						if pattern == "custom" {
							h = tsdbutil.GenerateTestCustomBucketsHistogram(int64(i))
						}
						if pattern == "gauge" || pattern == "large" || pattern == "special" {
							h.CounterResetHint = histogram.GaugeType
						}
						if pattern == "large" {
							h.Count = math.MaxUint64 - uint64(i)
							h.ZeroCount = h.Count
							h.PositiveBuckets = nil
							h.NegativeBuckets = nil
							h.PositiveSpans = nil
							h.NegativeSpans = nil
						}
						if pattern == "reset" && i%19 == 0 {
							h.CounterResetHint = histogram.CounterReset
						}
						if pattern == "layout" && i > 60 {
							h.PositiveSpans[0].Length++
							h.PositiveBuckets = append(h.PositiveBuckets, 0)
						}
						if pattern == "stale" && i%17 == 0 {
							h = &histogram.Histogram{Sum: math.Float64frombits(value.StaleNaN)}
						}
						if pattern == "special" {
							h.Sum = []float64{math.Copysign(0, -1), math.Inf(1), math.SmallestNonzeroFloat64, math.Float64frombits(0x7ff8123456789abc)}[i%4]
						}
						var next Chunk
						var recoded bool
						if enc == EncALPHistogram || enc == EncHistogramST {
							next, recoded, a, err = a.AppendHistogram(nil, int64(i/10), int64(i*15000), h, false)
						} else {
							fh := h.ToFloat(nil)
							if pattern == "gauge" {
								for j := range fh.PositiveBuckets {
									fh.PositiveBuckets[j] += float64(i%7) / 100
								}
							}
							next, recoded, a, err = a.AppendFloatHistogram(nil, int64(i/10), int64(i*15000), fh, false)
						}
						require.NoError(t, err)
						if next != nil {
							if recoded {
								result[len(result)-1] = next
							} else {
								result = append(result, next)
							}
						}
					}
					return result
				}
				legacy := EncHistogramST
				if enc == EncALPFloatHistogram {
					legacy = EncFloatHistogramST
				}
				want, got := build(legacy), build(enc)
				require.Len(t, got, len(want))
				for i, c := range got {
					var encoder ALPEncoder
					v3, err := encoder.RecodeHistogramV3(want[i])
					require.NoError(t, err)
					variants := []Chunk{c, v3}
					if enc == EncALPHistogram {
						v2, err := RecodeToALPHistogramV2(want[i])
						require.NoError(t, err)
						variants = append(variants, v2)
					}
					for _, c := range variants {
						snapshot := c.Iterator(nil)
						serialized := slices.Clone(c.Bytes())
						c.Compact()
						loaded, err := FromData(enc, serialized)
						require.NoError(t, err)
						_, err = loaded.Appender()
						require.NoError(t, err)
						for _, it := range []Iterator{snapshot, loaded.Iterator(nil), c.Iterator(nil)} {
							ref := want[i].Iterator(nil)
							for typ := ref.Next(); typ != ValNone; typ = ref.Next() {
								require.Equal(t, typ, it.Next())
								require.Equal(t, ref.AtST(), it.AtST())
								require.Equal(t, ref.AtT(), it.AtT())
								if typ == ValHistogram {
									_, expected := ref.AtHistogram(nil)
									_, actual := it.AtHistogram(nil)
									require.Equal(t, math.Float64bits(expected.Sum), math.Float64bits(actual.Sum))
									expected.Sum, actual.Sum = 0, 0
									require.Equal(t, expected, actual)
								} else {
									_, expected := ref.AtFloatHistogram(nil)
									_, actual := it.AtFloatHistogram(nil)
									require.Equal(t, math.Float64bits(expected.Sum), math.Float64bits(actual.Sum))
									expected.Sum, actual.Sum = 0, 0
									require.Equal(t, expected, actual)
								}
							}
							// The legacy iterator may lend its current histogram; only its sum was changed above.
							require.NoError(t, ref.Err())
							require.Equal(t, ValNone, it.Next())
							require.NoError(t, it.Err())
						}
					}
				}
			})
		}
	}
}

func TestALPHistogramCorruption(t *testing.T) {
	for _, enc := range []Encoding{EncALPHistogram, EncALPFloatHistogram} {
		c, err := NewEmptyChunk(enc)
		require.NoError(t, err)
		a, err := c.Appender()
		require.NoError(t, err)
		for i := range 20 {
			if enc == EncALPHistogram {
				_, _, a, err = a.AppendHistogram(nil, 1, int64(i), tsdbutil.GenerateTestHistogram(int64(i)), true)
			} else {
				_, _, a, err = a.AppendFloatHistogram(nil, 1, int64(i), tsdbutil.GenerateTestFloatHistogram(int64(i)), true)
			}
			require.NoError(t, err)
		}
		var encoder ALPEncoder
		v3, err := encoder.RecodeHistogramV3(c.(*ALPHistogramChunk).inner)
		require.NoError(t, err)
		variants := [][]byte{c.Bytes(), v3.Bytes()}
		if enc == EncALPHistogram {
			v2, err := RecodeToALPHistogramV2(c.(*ALPHistogramChunk).inner)
			require.NoError(t, err)
			variants = append(variants, v2.Bytes())
		}
		for _, b := range variants {
			for size := 1; size < len(b); size++ {
				broken, err := FromData(enc, b[:size])
				if err != nil {
					continue
				}
				it := broken.Iterator(nil)
				for it.Next() != ValNone {
				}
				require.Error(t, it.Err(), "size %d", size)
				_, err = broken.Appender()
				require.Error(t, err)
			}
		}
	}
}

func FuzzALPHistogramDecode(f *testing.F) {
	for _, enc := range []Encoding{EncALPHistogram, EncALPFloatHistogram} {
		c, _ := NewEmptyChunk(enc)
		a, _ := c.Appender()
		if enc == EncALPHistogram {
			_, _, _, _ = a.AppendHistogram(nil, 1, 2, tsdbutil.GenerateTestHistogram(0), true)
		} else {
			_, _, _, _ = a.AppendFloatHistogram(nil, 1, 2, tsdbutil.GenerateTestFloatHistogram(0), true)
		}
		f.Add(byte(enc), c.Bytes())
		var encoder ALPEncoder
		v3, _ := encoder.RecodeHistogramV3(c.(*ALPHistogramChunk).inner)
		f.Add(byte(enc), v3.Bytes())
		if enc == EncALPHistogram {
			v2, _ := RecodeToALPHistogramV2(c.(*ALPHistogramChunk).inner)
			f.Add(byte(enc), v2.Bytes())
		}
	}
	f.Fuzz(func(_ *testing.T, e byte, b []byte) {
		if len(b) > 16384 {
			return
		}
		enc := EncALPHistogram
		if e%2 != 0 {
			enc = EncALPFloatHistogram
		}
		c, err := FromData(enc, b)
		if err != nil {
			return
		}
		it := c.Iterator(nil)
		for it.Next() != ValNone {
			it.AtFloatHistogram(nil)
		}
		_ = it.Err()
	})
}

func BenchmarkALPHistograms(b *testing.B) {
	hs := tsdbutil.GenerateTestHistograms(120)
	fhs := tsdbutil.GenerateTestFloatHistograms(120)
	for _, variant := range []struct {
		enc     Encoding
		version byte
	}{{EncHistogramST, 0}, {EncALPHistogram, 1}, {EncFloatHistogramST, 0}, {EncALPFloatHistogram, 1}, {EncALPHistogram, 2}, {EncALPHistogram, 3}, {EncALPFloatHistogram, 3}} {
		enc := variant.enc
		name := enc.String()
		if variant.version > 1 {
			name += "V" + strconv.Itoa(int(variant.version))
		}
		b.Run(name, func(b *testing.B) {
			build := func() Chunk {
				c, _ := NewEmptyChunk(enc)
				if variant.version > 0 {
					c.(*ALPHistogramChunk).version = variant.version
				}
				a, _ := c.Appender()
				for i := range 120 {
					var err error
					if enc == EncHistogramST || enc == EncALPHistogram {
						_, _, a, err = a.AppendHistogram(nil, 1, int64(i*15000), hs[i], true)
					} else {
						_, _, a, err = a.AppendFloatHistogram(nil, 1, int64(i*15000), fhs[i], true)
					}
					if err != nil {
						b.Fatal(err)
					}
				}
				return c
			}
			data := build().Bytes()
			c, err := FromData(enc, data)
			require.NoError(b, err)
			b.Run("read", func(b *testing.B) {
				var it Iterator
				var h *histogram.Histogram
				var fh *histogram.FloatHistogram
				b.ReportAllocs()
				for b.Loop() {
					it = c.Iterator(it)
					for typ := it.Next(); typ != ValNone; typ = it.Next() {
						if typ == ValHistogram {
							_, h = it.AtHistogram(h)
						} else {
							_, fh = it.AtFloatHistogram(fh)
						}
					}
					if it.Err() != nil {
						b.Fatal(it.Err())
					}
				}
				b.ReportMetric(float64(len(data))/120, "encoded-B/sample")
			})
			b.Run("write", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					alpBenchSink = float64(len(build().Bytes()))
				}
				b.ReportMetric(float64(len(data))/120, "encoded-B/sample")
			})
		})
	}
}

func TestALPHistogramV3WideFirstSample(t *testing.T) {
	for _, fields := range []int{0, 1, 127, 128, 129, 1025} {
		c := NewHistogramSTChunk()
		a, _ := c.Appender()
		h := &histogram.Histogram{CounterResetHint: histogram.GaugeType, Schema: 1, PositiveSpans: []histogram.Span{{Length: uint32(fields)}}, PositiveBuckets: make([]int64, fields)}
		for i := range h.PositiveBuckets {
			h.PositiveBuckets[i] = int64(i%19) - 9
		}
		for i := range 5 {
			h.Count = math.MaxUint64 - uint64(i)
			_, _, a, _ = a.AppendHistogram(nil, 1, int64(i), h, true)
		}
		var e ALPEncoder
		encoded, err := e.RecodeHistogramV3(c)
		require.NoError(t, err)
		ref, it := c.Iterator(nil), encoded.Iterator(nil)
		for ref.Next() != ValNone {
			require.Equal(t, ValHistogram, it.Next())
			_, want := ref.AtHistogram(nil)
			_, got := it.AtHistogram(nil)
			require.Equal(t, want, got)
		}
		require.Equal(t, ValNone, it.Next())
		require.NoError(t, it.Err())
	}
}

func TestALPHistogramV3HintPadding(t *testing.T) {
	c := NewHistogramSTChunk()
	a, _ := c.Appender()
	_, _, _, err := a.AppendHistogram(nil, 1, 1, tsdbutil.GenerateTestHistogram(0), true)
	require.NoError(t, err)
	var e ALPEncoder
	encoded, err := e.RecodeHistogramV3(c)
	require.NoError(t, err)
	data := slices.Clone(encoded.Bytes())
	var it alpHistogramIterator
	it.reset(data, EncALPHistogram)
	require.NoError(t, it.Err())
	require.Len(t, it.hints, 1)
	// Hints borrow the input bytes, so this corrupts only the unused high bits.
	it.hints[0] |= 0x80
	it.reset(data, EncALPHistogram)
	require.Error(t, it.Err())
}
