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
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prometheus/prometheus/model/value"
)

var alpBenchSink float64

// BenchmarkALPChunk includes timestamps, serialization, and iterator setup so
// codec comparisons do not measure only the decimal reconstruction kernel.
func BenchmarkALPChunk(b *testing.B) {
	const n = 120
	for _, pattern := range []string{"constant", "decimal", "exceptions", "computed", "random"} {
		values := make([]float64, n)
		r := rand.New(rand.NewSource(42))
		for i := range values {
			switch pattern {
			case "constant":
				values[i] = 1
			case "decimal", "exceptions":
				values[i] = float64(100000+i) / 100
				if pattern == "exceptions" && i%20 == 0 {
					values[i] = math.Float64frombits(value.StaleNaN)
				}
			case "computed":
				values[i] = 1 + math.Sin(float64(i))/10
			case "random":
				values[i] = math.Float64frombits(r.Uint64())
			}
		}
		for _, enc := range []Encoding{EncXOR, EncXOR2, EncALP} {
			b.Run(pattern+"/"+enc.String(), func(b *testing.B) {
				makeChunk := func() Chunk {
					c, err := NewEmptyChunk(enc)
					require.NoError(b, err)
					a, err := c.Appender()
					require.NoError(b, err)
					for i, v := range values {
						a.Append(0, 1_750_000_000_000+int64(i)*15000, v)
					}
					return c
				}
				encoded := makeChunk().Bytes()
				c, err := FromData(enc, encoded)
				require.NoError(b, err)
				b.Run("read", func(b *testing.B) {
					var it Iterator
					b.SetBytes(n * 8)
					b.ReportAllocs()
					for b.Loop() {
						it = c.Iterator(it)
						for it.Next() != ValNone {
							_, alpBenchSink = it.At()
						}
						if err := it.Err(); err != nil {
							b.Fatal(err)
						}
					}
					b.ReportMetric(float64(len(encoded))/n, "encoded-B/sample")
				})
				b.Run("write", func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						chunk := makeChunk()
						alpBenchSink = float64(len(chunk.Bytes()))
					}
					b.ReportMetric(float64(len(encoded))/n, "encoded-B/sample")
				})
			})
		}
	}
}

func BenchmarkALPDecode(b *testing.B) {
	for _, n := range []int{120, 128, 1024} {
		for _, pattern := range []string{"decimal", "exceptions", "computed"} {
			values := make([]float64, n)
			for i := range values {
				values[i] = float64(100000+i) / 100
				if pattern == "exceptions" && i%20 == 0 {
					values[i] = math.Float64frombits(value.StaleNaN)
				}
				if pattern == "computed" {
					values[i] = 1 + math.Sin(float64(i))/10
				}
			}
			encoded := alpEncodeValues(nil, values)
			for _, backend := range []struct {
				name   string
				kernel alpKernel
			}{{"scalar", alpDecodeScalar}, {"native_" + alpBackend, alpDecodeNative}} {
				b.Run(fmt.Sprintf("%s/%d/%s", pattern, n, backend.name), func(b *testing.B) {
					var scratch alpDecodeScratch
					dst := make([]float64, n)
					require.NoError(b, alpDecodeValues(dst, encoded, &scratch, backend.kernel))
					b.ReportAllocs()
					b.SetBytes(int64(n * 8))
					for b.Loop() {
						if err := alpDecodeValues(dst, encoded, &scratch, backend.kernel); err != nil {
							b.Fatal(err)
						}
						alpBenchSink = dst[n-1]
					}
					b.ReportMetric(float64(len(encoded))/float64(n), "encoded-B/value")
				})
			}
		}
	}
}

func BenchmarkALPKernel(b *testing.B) {
	for _, width := range []int{0, 7, 10, 16, 31, 63} {
		for _, n := range []int{128, 1024} {
			residuals := make([]uint64, n)
			for i := range residuals {
				residuals[i] = uint64(i) & alpMask(width)
			}
			words := alpUnpackWords(nil, alpPack(nil, residuals, width), n, width)
			for _, backend := range []struct {
				name   string
				kernel alpKernel
			}{{"scalar", alpDecodeScalar}, {"native_" + alpBackend, alpDecodeNative}} {
				b.Run(fmt.Sprintf("width%d/%d/%s", width, n, backend.name), func(b *testing.B) {
					dst := make([]float64, n)
					b.ReportAllocs()
					b.SetBytes(int64(n * 8))
					for b.Loop() {
						backend.kernel(dst, words, width, 1000, 0, 2)
						alpBenchSink = dst[n-1]
					}
				})
			}
		}
	}
}

func BenchmarkALPEncode(b *testing.B) {
	for _, n := range []int{120, 128, 1024} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			values := make([]float64, n)
			for i := range values {
				values[i] = float64(100000+i) / 100
			}
			dst := make([]byte, 0, n*8+32)
			b.ReportAllocs()
			for b.Loop() {
				dst = alpEncodeValues(dst[:0], values)
			}
			b.ReportMetric(float64(len(dst))/float64(n), "encoded-B/value")
		})
	}
}
