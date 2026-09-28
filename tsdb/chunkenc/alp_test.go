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
	"fmt"
	"math"
	"math/rand"
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prometheus/prometheus/model/value"
)

func TestALPValues(t *testing.T) {
	t.Run("decimal wire format", func(t *testing.T) {
		values := make([]float64, 16)
		for i := range values {
			values[i] = float64(i)
		}
		// Width four, exponent/factor/base zero, no exceptions, one value per lane.
		want := []byte{3, 4, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
		require.Equal(t, want, alpEncodeValues(nil, values))
		var scratch alpDecodeScratch
		got := make([]float64, len(values))
		require.NoError(t, alpDecodeValues(got, want, &scratch, alpDecodeNative))
		require.Equal(t, values, got)
	})
	special := []float64{0, math.Copysign(0, -1), math.Inf(1), math.Inf(-1), math.SmallestNonzeroFloat64, -math.SmallestNonzeroFloat64, math.MaxFloat64, -math.MaxFloat64, math.Float64frombits(value.StaleNaN), math.Float64frombits(0x7ff0000000000001), math.Float64frombits(0xfff8123456789abc), 0x1p63, -0x1p63, 0x1p53 - 1, -0x1p51, 0.1, -0.1}
	for _, n := range []int{1, 2, 7, 15, 16, 17, 31, 32, 33, 63, 64, 65, 119, 120, 127, 128, 129, 240, 1023, 1024} {
		for _, pattern := range []string{"decimal", "integer", "constant", "special", "random", "computed"} {
			t.Run(fmt.Sprintf("%s/%d", pattern, n), func(t *testing.T) {
				r := rand.New(rand.NewSource(42))
				values := make([]float64, n)
				for i := range values {
					switch pattern {
					case "decimal":
						values[i] = float64(i-60) / 100
					case "integer":
						values[i] = float64(i - 500)
					case "constant":
						values[i] = math.Float64frombits(value.StaleNaN)
					case "special":
						values[i] = special[i%len(special)]
					case "random":
						values[i] = math.Float64frombits(r.Uint64())
					case "computed":
						values[i] = 1 + r.Float64()
					}
				}
				encoded := alpEncodeValues(nil, values)
				require.LessOrEqual(t, len(encoded), 1+8*n)
				var scratch alpDecodeScratch
				for _, kernel := range []alpKernel{alpDecodeScalar, alpDecodeNative} {
					got := make([]float64, n)
					require.NoError(t, alpDecodeValues(got, encoded, &scratch, kernel))
					for i := range values {
						require.Equal(t, math.Float64bits(values[i]), math.Float64bits(got[i]), "sample %d", i)
					}
				}
			})
		}
	}
}

func TestALPPacking(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	for width := 0; width <= 64; width++ {
		for n := 1; n <= 129; n++ {
			values := make([]uint64, n)
			for i := range values {
				values[i] = r.Uint64() & alpMask(width)
			}
			packed := alpPack(nil, values, width)
			require.Len(t, packed, alpPackedSize(n, width))
			words := alpUnpackWords(nil, packed, n, width)
			for i, want := range values {
				require.Equal(t, want, alpUnpackAt(words, i, width), "width %d count %d index %d", width, n, i)
			}
		}
	}
	// One bit per value: lanes 0 and 2 are set in the compact suffix.
	require.Equal(t, []byte{1, 0, 1}, alpPack(nil, []uint64{1, 0, 1}, 1))
	// A complete eight-value lane occupies exactly ten bytes at width ten.
	values := make([]uint64, 128)
	values[0], values[16], values[112] = 1, 2, 3
	packed := alpPack(nil, values, 10)
	require.Len(t, packed, 160)
	require.Equal(t, uint64(2049), binary.LittleEndian.Uint64(packed))
	require.Equal(t, []byte{192, 0}, packed[128:130])
}

func TestALPKernels(t *testing.T) {
	r := rand.New(rand.NewSource(123))
	for width := 0; width <= 64; width++ {
		for _, n := range []int{1, 3, 7, 15, 17, 120, 128, 1024} {
			residuals := make([]uint64, n)
			for i := range residuals {
				residuals[i] = r.Uint64() & alpMask(width)
			}
			words := alpUnpackWords(nil, alpPack(nil, residuals, width), n, width)
			for _, base := range []int64{0, -0x1p51, math.MinInt64} {
				got, want := make([]float64, n), make([]float64, n)
				alpDecodeScalar(want, words, width, base, 0, 3)
				alpDecodeNative(got, words, width, base, 0, 3)
				for i := range got {
					require.Equal(t, math.Float64bits(want[i]), math.Float64bits(got[i]), "backend %s width %d index %d", alpBackend, width, i)
				}
			}
		}
	}
	for factor := range uint8(len(alpFactors)) {
		residuals := []uint64{0, 1, 2, 3, 4, 5, 6, 7}
		words := alpUnpackWords(nil, alpPack(nil, residuals, 3), len(residuals), 3)
		got, want := make([]float64, 8), make([]float64, 8)
		alpDecodeScalar(want, words, 3, -4, factor, factor)
		alpDecodeNative(got, words, 3, -4, factor, factor)
		for i := range got {
			require.Equal(t, math.Float64bits(want[i]), math.Float64bits(got[i]))
		}
	}
}

func TestALPLifecycle(t *testing.T) {
	t.Run("capacity", func(t *testing.T) {
		c := NewALPChunk()
		a, err := c.Appender()
		require.NoError(t, err)
		for i := range math.MaxUint16 {
			a.Append(0, int64(i), 1)
		}
		require.True(t, IsFloatChunkFull(c))
		require.Panics(t, func() { a.Append(0, math.MaxUint16, 1) })
		decoded, err := FromData(EncALP, c.Bytes())
		require.NoError(t, err)
		require.Equal(t, math.MaxUint16, decoded.NumSamples())
		it := decoded.Iterator(nil)
		for i := range math.MaxUint16 {
			require.Equal(t, ValFloat, it.Next())
			require.Equal(t, int64(i), it.AtT())
		}
		require.Equal(t, ValNone, it.Next())
		require.NoError(t, it.Err())
	})
	for _, boundary := range []int{0, 1, 17, 120, 127, 128, 129, 240} {
		t.Run(strconv.Itoa(boundary), func(t *testing.T) {
			c := NewALPChunk()
			a, err := c.Appender()
			require.NoError(t, err)
			for i := range boundary {
				a.Append(int64(i/7), int64(i*15000+i%3), float64(i)/100)
			}
			snapshot := c.Iterator(nil)
			encoded := c.Bytes()
			before := slices.Clone(encoded)
			c.Compact()
			a, err = c.Appender()
			require.NoError(t, err)
			a.Append(123, 1e9, math.Float64frombits(value.StaleNaN))
			require.Equal(t, before, encoded)
			count := 0
			for snapshot.Next() != ValNone {
				require.Equal(t, int64(count*15000+count%3), snapshot.AtT())
				count++
			}
			require.NoError(t, snapshot.Err())
			require.Equal(t, boundary, count)
			c.Reset(c.Bytes())
			it := c.Iterator(snapshot)
			for i := range boundary {
				require.Equal(t, ValFloat, it.Next())
				ts, v := it.At()
				require.Equal(t, int64(i*15000+i%3), ts)
				require.Equal(t, int64(i/7), it.AtST())
				require.Equal(t, math.Float64bits(float64(i)/100), math.Float64bits(v))
			}
			require.Equal(t, ValFloat, it.Next())
			_, v := it.At()
			require.Equal(t, value.StaleNaN, math.Float64bits(v))
			require.Equal(t, int64(123), it.AtST())
			require.Equal(t, ValNone, it.Next())
			require.NoError(t, it.Err())
			pool := NewPool()
			require.NoError(t, pool.Put(c))
			require.Zero(t, c.NumSamples())
			pooled, err := pool.Get(EncALP, before)
			require.NoError(t, err)
			require.Equal(t, boundary, pooled.NumSamples())
		})
	}
}

func TestALPSnapshotConcurrentAppend(t *testing.T) {
	c := NewALPChunk()
	a, err := c.Appender()
	require.NoError(t, err)
	for i := range 200 {
		a.Append(1, int64(i), float64(i)/100)
	}
	it := c.Iterator(nil)
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := 200; i < 1000; i++ {
			a.Append(1, int64(i), float64(i)/100)
		}
	})
	for i := range 200 {
		require.Equal(t, ValFloat, it.Next())
		require.Equal(t, int64(i), it.AtT())
	}
	require.Equal(t, ValNone, it.Next())
	require.NoError(t, it.Err())
	wg.Wait()
}

func TestALPCorruption(t *testing.T) {
	t.Run("decimal arithmetic", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			base     int64
			residual uint64
			factor   byte
		}{
			{"frame overflow", math.MaxInt64, 1, 0},
			{"positive product overflow", math.MaxInt64 / 10, 1, 1},
			{"negative product overflow", math.MinInt64/10 - 1, 0, 1},
		} {
			t.Run(tc.name, func(t *testing.T) {
				data := []byte{alpDecimal, 64, tc.factor, tc.factor}
				data = binary.LittleEndian.AppendUint64(data, uint64(tc.base))
				data = binary.LittleEndian.AppendUint16(data, 0)
				data = alpPack(data, []uint64{tc.residual}, 64)
				var scratch alpDecodeScratch
				require.Error(t, alpDecodeValues(make([]float64, 1), data, &scratch, alpDecodeNative))
			})
		}
	})
	c := NewALPChunk()
	a, err := c.Appender()
	require.NoError(t, err)
	for i := range 120 {
		a.Append(1, int64(i), float64(i)/100)
	}
	data := slices.Clone(c.Bytes())
	for length := 1; length < len(data); length++ {
		broken := NewALPChunk()
		broken.Reset(data[:length])
		it := broken.Iterator(nil)
		for it.Next() != ValNone {
		}
		require.Error(t, it.Err(), "length %d", length)
		_, err := broken.Appender()
		require.Error(t, err)
	}
	for _, offset := range []int{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14} {
		broken := slices.Clone(data)
		broken[offset] = 255
		c.Reset(broken)
		it := c.Iterator(nil)
		for it.Next() != ValNone {
		}
		require.Error(t, it.Err(), "offset %d", offset)
	}
}

func FuzzALPRoundTrip(f *testing.F) {
	for _, data := range [][]byte{{0}, {1, 2, 3}, {0xff, 0x7f, 0xf0, 1}, make([]byte, 128)} {
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 || len(data) > 8*alpMaxBlockSize {
			return
		}
		values := make([]float64, (len(data)+7)/8)
		for i := range values {
			var raw [8]byte
			copy(raw[:], data[i*8:])
			values[i] = math.Float64frombits(binary.LittleEndian.Uint64(raw[:]))
		}
		encoded := alpEncodeValues(nil, values)
		got := make([]float64, len(values))
		var scratch alpDecodeScratch
		require.NoError(t, alpDecodeValues(got, encoded, &scratch, alpDecodeNative))
		for i := range got {
			require.Equal(t, math.Float64bits(values[i]), math.Float64bits(got[i]))
		}
	})
}

func FuzzALPDecode(f *testing.F) {
	c := NewALPChunk()
	a, _ := c.Appender()
	for i := range 120 {
		a.Append(1, int64(i), float64(i)/10)
	}
	f.Add(c.Bytes())
	f.Add([]byte{0, 0, alpVersion})
	f.Fuzz(func(_ *testing.T, data []byte) {
		c := NewALPChunk()
		c.Reset(data)
		it := c.Iterator(nil)
		for it.Next() != ValNone {
			it.At()
			it.AtST()
		}
		_ = it.Err()
	})
}
