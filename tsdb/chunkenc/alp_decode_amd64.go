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

import "simd/archsimd"

var alpDecodeNative, alpBackend = alpSelectAMD64()

func alpSelectAMD64() (alpKernel, string) {
	if archsimd.X86.AVX512() {
		return alpDecodeAVX512, "avx512"
	}
	if archsimd.X86.AVX2() {
		return alpDecodeAVX2, "avx2"
	}
	return alpDecodeScalar, "scalar"
}

func alpDecodeAVX2Generic(dst []float64, words []uint64, width int, base int64, factor, exponent uint8) {
	mask := archsimd.BroadcastUint64x4(alpMask(width))
	frame := archsimd.BroadcastUint64x4(uint64(base))
	scale := archsimd.BroadcastFloat64x4(alpFractions[exponent])
	for row := 0; row*alpLanes < len(dst); row++ {
		bit := row * width
		word, shift := (bit/64)*alpLanes, uint64(bit%64)
		for lane := 0; lane < alpLanes && row*alpLanes+lane < len(dst); lane += 4 {
			x := archsimd.LoadUint64x4(words[word+lane:]).ShiftAllRight(shift)
			if shift+uint64(width) > 64 {
				x = x.Or(archsimd.LoadUint64x4(words[word+lane+alpLanes:]).ShiftAllLeft(64 - shift))
			}
			x = x.And(mask).Add(frame)
			if factor != 0 {
				x = alpMultiplyAVX2(x, uint64(alpFactors[factor]))
			}
			v := alpInt64ToFloat64AVX2(x).Mul(scale)
			out := dst[row*alpLanes+lane:]
			if len(out) >= 4 {
				v.Store(out)
			} else {
				v.StorePart(out)
			}
		}
	}
}

func alpMultiplyAVX2(x archsimd.Uint64x4, p uint64) archsimd.Uint64x4 {
	low := x.ReshapeToUint32s()
	high := x.ShiftAllRight(32).ReshapeToUint32s()
	plo := archsimd.BroadcastUint32x8(uint32(p))
	phi := archsimd.BroadcastUint32x8(uint32(p >> 32))
	cross := low.MulWidenEven(phi).Add(high.MulWidenEven(plo))
	return low.MulWidenEven(plo).Add(cross.ShiftAllLeft(32))
}

// alpInt64ToFloat64AVX2 converts the full signed range by constructing binary64
// representations of its low and high 32-bit halves. Subtraction must precede
// addition to preserve rounding. Direct Int64x4 conversion requires AVX-512.
func alpInt64ToFloat64AVX2(x archsimd.Uint64x4) archsimd.Float64x4 {
	low := x.And(archsimd.BroadcastUint64x4(0xffffffff)).Or(archsimd.BroadcastUint64x4(0x4330000000000000))
	high := x.ShiftAllRight(32).Xor(archsimd.BroadcastUint64x4(0x4530000080000000))
	const bias = 0x1p84 + 0x1p63 + 0x1p52 // Binary64 bits 0x4530000080100000.
	return high.BitsToFloat64().Sub(archsimd.BroadcastFloat64x4(bias)).Add(low.BitsToFloat64())
}

func alpDecodeAVX512Generic(dst []float64, words []uint64, width int, base int64, factor, exponent uint8) {
	mask := archsimd.BroadcastUint64x8(alpMask(width))
	frame := archsimd.BroadcastUint64x8(uint64(base))
	multiplier := archsimd.BroadcastInt64x8(alpFactors[factor])
	scale := archsimd.BroadcastFloat64x8(alpFractions[exponent])
	for row := 0; row*alpLanes < len(dst); row++ {
		bit := row * width
		word, shift := (bit/64)*alpLanes, uint64(bit%64)
		for lane := 0; lane < alpLanes && row*alpLanes+lane < len(dst); lane += 8 {
			x := archsimd.LoadUint64x8(words[word+lane:]).ShiftAllRight(shift)
			if shift+uint64(width) > 64 {
				x = x.Or(archsimd.LoadUint64x8(words[word+lane+alpLanes:]).ShiftAllLeft(64 - shift))
			}
			q := x.And(mask).Add(frame).BitsToInt64()
			v := q.Mul(multiplier).ConvertToFloat64().Mul(scale)
			out := dst[row*alpLanes+lane:]
			if len(out) >= 8 {
				v.Store(out)
			} else {
				v.StorePart(out)
			}
		}
	}
}
