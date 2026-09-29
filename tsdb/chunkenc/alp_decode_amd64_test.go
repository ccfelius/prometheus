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

//go:build go1.27 && goexperiment.simd && amd64

package chunkenc

import (
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestALPAMD64Backends(t *testing.T) {
	for _, backend := range []struct {
		name      string
		supported bool
		kernel    alpKernel
	}{
		{"avx2", alpBackend == "avx2" || alpBackend == "avx512", alpDecodeAVX2},
		{"avx512", alpBackend == "avx512", alpDecodeAVX512},
	} {
		t.Run(backend.name, func(t *testing.T) {
			if !backend.supported {
				t.Skip("CPU does not support this backend")
			}
			if backend.name == "avx2" {
				testALPEncodeConversions(t, alpConvertAVX2)
			} else {
				testALPEncodeConversions(t, alpConvertAVX512)
			}
			r := rand.New(rand.NewSource(42))
			for width := 0; width <= 64; width++ {
				for _, n := range []int{1, 3, 7, 15, 17, 120, 128, 1024} {
					values := make([]uint64, n)
					for i := range values {
						values[i] = r.Uint64() & alpMask(width)
					}
					words := alpUnpackWords(nil, alpPack(nil, values, width), n, width)
					want, got := make([]float64, n), make([]float64, n)
					alpDecodeScalar(want, words, width, math.MinInt64, 0, 3)
					backend.kernel(got, words, width, math.MinInt64, 0, 3)
					for i := range got {
						require.Equal(t, math.Float64bits(want[i]), math.Float64bits(got[i]))
					}
				}
			}
			for factor := range uint8(len(alpFactors)) {
				values := []uint64{0, 1, 2, 3, 4, 5, 6, 7}
				words := alpUnpackWords(nil, alpPack(nil, values, 3), len(values), 3)
				want, got := make([]float64, 8), make([]float64, 8)
				alpDecodeScalar(want, words, 3, -4, factor, factor)
				backend.kernel(got, words, 3, -4, factor, factor)
				for i := range got {
					require.Equal(t, math.Float64bits(want[i]), math.Float64bits(got[i]))
				}
			}
		})
	}
}
