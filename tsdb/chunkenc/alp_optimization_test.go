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
	"encoding/binary"
	"math"
	"math/rand"
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/value"
	"github.com/prometheus/prometheus/tsdb/tsdbutil"
)

func TestALPEncodeConversions(t *testing.T) { testALPEncodeConversions(t, alpConvertNative) }

func testALPEncodeConversions(t *testing.T, kernel alpEncodeKernel) {
	r := rand.New(rand.NewSource(51))
	values := []float64{0, math.Copysign(0, -1), math.Inf(1), math.Inf(-1), math.SmallestNonzeroFloat64, math.MaxFloat64, math.Float64frombits(value.StaleNaN), 0x1p63, -0x1p63, 0x1p53 - 1, -0x1p51, 0x1p51, 0x1p51 - 1}
	for range 256 {
		values = append(values, math.Float64frombits(r.Uint64()))
	}
	for _, v := range []float64{0x1p51, -0x1p51, 0x1p63, -0x1p63} {
		values = append(values, math.Nextafter(v, math.Inf(-1)), math.Nextafter(v, math.Inf(1)))
	}
	for e := range uint8(19) {
		for f := uint8(0); f <= e; f++ {
			input := slices.Clone(values)
			for _, q := range []int64{alpLower[f], alpLower[f] + 1, alpUpper[f] - 1, alpUpper[f]} {
				v := float64(q) * alpPowers[f] * alpFractions[e]
				input = append(input, v, math.Nextafter(v, math.Inf(-1)), math.Nextafter(v, math.Inf(1)))
			}
			for _, n := range []int{1, 3, 7, 8, 15, len(input)} {
				integers, accepted := make([]int64, n), make([]uint64, n)
				kernel(input[:n], integers, accepted, e, f)
				for i, v := range input[:n] {
					q, ok := alpEncodeNumber(v, e, f)
					require.Equal(t, ok, accepted[i] != 0, "e=%d f=%d i=%d bits=%x", e, f, i, math.Float64bits(v))
					if ok {
						require.Equal(t, q, integers[i])
					}
				}
			}
		}
	}
}

func TestALPEncoderChangingSeries(t *testing.T) {
	var encoder ALPEncoder
	for block := range 80 {
		c := NewXOR2Chunk()
		a, _ := c.Appender()
		expected := make([]float64, 120)
		for i := range expected {
			switch block / 16 {
			case 0:
				expected[i] = float64(100000+i) / 100
			case 1:
				expected[i] = float64(i) / 1e6
			case 2:
				expected[i] = math.Sin(float64(i))
			case 3:
				expected[i] = float64(i) * 1e10
			default:
				expected[i] = math.Float64frombits(uint64(i) * 0x9e3779b97f4a7c15)
			}
			a.Append(1, int64(block*120+i), expected[i])
		}
		encoded, err := encoder.Recode(c)
		require.NoError(t, err)
		it := encoded.Iterator(nil)
		for i, v := range expected {
			require.Equal(t, ValFloat, it.Next())
			ts, got := it.At()
			require.Equal(t, int64(block*120+i), ts)
			require.Equal(t, int64(1), it.AtST())
			require.Equal(t, math.Float64bits(v), math.Float64bits(got))
		}
		require.Equal(t, ValNone, it.Next())
		require.NoError(t, it.Err())
	}
}

func TestALPHistogramMutableSnapshot(t *testing.T) {
	for _, floating := range []bool{false, true} {
		c := NewALPHistogramChunk()
		if floating {
			c = NewALPFloatHistogramChunk()
		}
		a, _ := c.Appender()
		appendSample := func(i int) {
			var err error
			if floating {
				_, _, a, err = a.AppendFloatHistogram(nil, 1, int64(i), tsdbutil.GenerateTestFloatHistogram(int64(i)), true)
			} else {
				_, _, a, err = a.AppendHistogram(nil, 1, int64(i), tsdbutil.GenerateTestHistogram(int64(i)), true)
			}
			require.NoError(t, err)
		}
		for i := range 60 {
			appendSample(i)
		}
		it := c.Iterator(nil)
		require.Nil(t, c.encoded, "snapshot must not serialize ALP")
		var wg sync.WaitGroup
		wg.Go(func() {
			for i := 60; i < 120; i++ {
				appendSample(i)
			}
		})
		count := 0
		for it.Next() != ValNone {
			ts, h := it.AtFloatHistogram(nil)
			require.Equal(t, int64(count), ts)
			require.Equal(t, tsdbutil.GenerateTestFloatHistogram(int64(count)).Sum, h.Sum)
			require.Equal(t, int64(1), it.AtST())
			count++
		}
		wg.Wait()
		require.Equal(t, 60, count)
		require.NoError(t, it.Err())
		require.Equal(t, 120, c.NumSamples())
		require.Nil(t, c.encoded)
		require.NotEmpty(t, c.Bytes())
	}
}

// BenchmarkALPIntegerVectorSize isolates the histogram numeric stream. It
// includes the first raw sample and each vector's length prefix, but no layout.
func BenchmarkALPIntegerVectorSize(b *testing.B) {
	hs := tsdbutil.GenerateTestHistograms(120)
	var raw []uint64
	var previous, delta []uint64
	for _, h := range hs {
		fields := []uint64{h.Count, h.ZeroCount}
		for _, buckets := range [][]int64{h.PositiveBuckets, h.NegativeBuckets} {
			for _, v := range buckets {
				fields = append(fields, uint64(v<<1)^uint64(v>>63))
			}
		}
		if previous == nil {
			previous = slices.Clone(fields)
			delta = make([]uint64, len(fields))
			continue
		}
		for i, v := range fields {
			d := v - previous[i]
			dd := int64(d - delta[i])
			previous[i], delta[i] = v, d
			raw = append(raw, uint64(dd<<1)^uint64(dd>>63))
		}
	}
	for _, size := range []int{64, 128, 256, 512, 1024} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			dst := make([]byte, 0, len(raw)*8)
			b.ReportAllocs()
			for b.Loop() {
				dst = dst[:0]
				for start := 0; start < len(raw); start += size {
					pos := len(dst)
					dst = append(dst, 0, 0, 0, 0)
					dst = alpEncodeIntegers(dst, raw[start:min(start+size, len(raw))])
					binary.LittleEndian.PutUint32(dst[pos:], uint32(len(dst)-pos-4))
				}
			}
			b.ReportMetric(float64(len(dst)+len(previous)*8)/120, "numeric-B/sample")
		})
	}
}

func TestALPHistogramV2Resume(t *testing.T) {
	source := NewHistogramSTChunk()
	a, _ := source.Appender()
	for i := range 40 {
		_, _, a, _ = a.AppendHistogram(nil, 1, int64(i), tsdbutil.GenerateTestHistogram(int64(i)), true)
	}
	c, err := RecodeToALPHistogramV2(source)
	require.NoError(t, err)
	require.Equal(t, byte(2), c.Bytes()[2])
	snapshot := c.Iterator(nil)
	c, err = FromData(EncALPHistogram, slices.Clone(c.Bytes()))
	require.NoError(t, err)
	a, err = c.Appender()
	require.NoError(t, err)
	for i := 40; i < 130; i++ {
		_, _, a, err = a.AppendHistogram(nil, 1, int64(i), tsdbutil.GenerateTestHistogram(int64(i)), true)
		require.NoError(t, err)
	}
	require.Equal(t, byte(2), c.Bytes()[2])
	c.Compact()
	for j, it := range []Iterator{snapshot, c.Iterator(nil)} {
		count := 0
		for it.Next() != ValNone {
			ts, h := it.AtHistogram(nil)
			require.Equal(t, int64(count), ts)
			want := tsdbutil.GenerateTestHistogram(int64(count))
			want.CounterResetHint = h.CounterResetHint
			require.Equal(t, want, h)
			count++
		}
		expected := 40
		if j == 1 {
			expected = 130
		}
		require.Equal(t, expected, count)
		require.NoError(t, it.Err())
	}
}

func TestALPEncodeReductionsAndPrediction(t *testing.T) {
	r := rand.New(rand.NewSource(904))
	for n := 0; n <= 259; n++ {
		q := make([]int64, n)
		accepted := make([]uint64, n)
		previous := make([]uint64, n)
		delta := make([]uint64, n)
		fields := make([]uint64, n)
		for i := range q {
			q[i] = int64(r.Uint64())
			accepted[i] = []uint64{0, 1, math.MaxUint64}[r.Intn(3)]
			previous[i] = r.Uint64()
			delta[i] = r.Uint64()
			fields[i] = r.Uint64()
		}
		if n > 1 {
			q[0], q[1] = math.MinInt64, math.MaxInt64
		}
		lo, hi, missing := alpReduceScalar(q, accepted)
		gotLo, gotHi, gotMissing := alpReduceNative(q, accepted)
		require.Equal(t, lo, gotLo)
		require.Equal(t, hi, gotHi)
		require.Equal(t, missing, gotMissing)
		p, d, v := slices.Clone(previous), slices.Clone(delta), slices.Clone(fields)
		alpPredictIntegersScalar(p, d, v)
		alpPredictIntegersNative(previous, delta, fields)
		require.Equal(t, p, previous)
		require.Equal(t, d, delta)
		require.Equal(t, v, fields)
		clear(accepted)
		lo, hi, missing = alpReduceNative(q, accepted)
		require.Equal(t, int64(math.MaxInt64), lo)
		require.Equal(t, int64(math.MinInt64), hi)
		require.Equal(t, n, missing)
	}
}

func TestALPHistogramRepeatedMaterialization(t *testing.T) {
	for _, floating := range []bool{false, true} {
		for _, wide := range []bool{false, true} {
			enc := EncHistogramST
			if floating {
				enc = EncFloatHistogramST
			}
			source, _ := NewEmptyChunk(enc)
			a, _ := source.Appender()
			for i := range 129 {
				h := tsdbutil.GenerateTestHistogram(int64(i))
				h.CounterResetHint = histogram.GaugeType
				if wide {
					h.PositiveSpans = []histogram.Span{{Length: 1031}}
					h.PositiveBuckets = make([]int64, 1031)
					for j := range h.PositiveBuckets {
						h.PositiveBuckets[j] = int64(j % 3)
					}
				}
				var err error
				if floating {
					fh := h.ToFloat(nil)
					for j := range fh.PositiveBuckets {
						fh.PositiveBuckets[j] += float64((i+j)%7) / 100
					}
					_, _, a, err = a.AppendFloatHistogram(nil, 1, int64(i), fh, true)
				} else {
					_, _, a, err = a.AppendHistogram(nil, 1, int64(i), h, true)
				}
				require.NoError(t, err)
			}
			var encoder ALPEncoder
			c, err := encoder.RecodeHistogramV3(source)
			require.NoError(t, err)
			it, ref := c.Iterator(nil), source.Iterator(nil)
			for ref.Next() != ValNone {
				require.NotEqual(t, ValNone, it.Next())
				_, want := ref.AtFloatHistogram(nil)
				_, first := it.AtFloatHistogram(nil)
				require.Equal(t, want, first)
				first.PositiveBuckets[0] = -999
				first.PositiveSpans[0].Offset = 999
				_, again := it.AtFloatHistogram(nil)
				require.Equal(t, want, again)
				if !floating {
					_, wantInt := ref.AtHistogram(nil)
					_, got := it.AtHistogram(nil)
					require.Equal(t, wantInt, got)
					got.PositiveBuckets[0] = -888
					_, got = it.AtHistogram(got)
					require.Equal(t, wantInt, got)
					_, again = it.AtFloatHistogram(again)
					require.Equal(t, want, again)
				}
			}
			require.Equal(t, ValNone, it.Next())
			require.NoError(t, it.Err())
		}
	}
}
