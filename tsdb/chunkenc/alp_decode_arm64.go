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

//go:build go1.27 && goexperiment.simd && arm64

package chunkenc

import "simd/archsimd"

var alpDecodeNative alpKernel = alpDecodeNEON

const alpBackend = "neon"

func alpDecodeNEONGeneric(dst []float64, words []uint64, width int, base int64, factor, exponent uint8) {
	mask := archsimd.BroadcastUint64x2(alpMask(width))
	frame := archsimd.BroadcastUint64x2(uint64(base))
	scale := archsimd.BroadcastFloat64x2(alpFractions[exponent])
	for row := 0; row*alpLanes < len(dst); row++ {
		bit := row * width
		word, shift := (bit/64)*alpLanes, uint64(bit%64)
		for lane := 0; lane < alpLanes && row*alpLanes+lane < len(dst); lane += 2 {
			x := archsimd.LoadUint64x2(words[word+lane:]).ShiftAllRight(shift)
			if shift+uint64(width) > 64 {
				x = x.Or(archsimd.LoadUint64x2(words[word+lane+alpLanes:]).ShiftAllLeft(64 - shift))
			}
			x = x.And(mask).Add(frame)
			if factor != 0 {
				x = alpMultiplyNEON(x, uint64(alpFactors[factor]))
			}
			v := x.BitsToInt64().ConvertToFloat64().Mul(scale)
			out := dst[row*alpLanes+lane:]
			if len(out) >= 2 {
				v.Store(out)
			} else {
				v.StorePart(out)
			}
		}
	}
}

// alpMultiplyNEON synthesizes a modulo-2^64 integer product. The caller proves
// the final signed product is representable; intermediate terms may wrap.
func alpMultiplyNEON(x archsimd.Uint64x2, p uint64) archsimd.Uint64x2 {
	half := x.ReshapeToUint32s()
	low, high := half.ConcatEven(half), half.ConcatOdd(half)
	plo := archsimd.BroadcastUint32x4(uint32(p))
	phi := archsimd.BroadcastUint32x4(uint32(p >> 32))
	cross := low.Mul(phi).Add(high.Mul(plo))
	upper := archsimd.BroadcastUint32x4(0).InterleaveLo(cross).ReshapeToUint64s()
	return low.MulWidenLo(plo).Add(upper)
}
