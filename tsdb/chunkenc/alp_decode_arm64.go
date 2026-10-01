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

// alpConvertNEON performs exact candidate conversion in 2 lanes.
func alpConvertNEON(values []float64, integers []int64, accepted []uint64, exponent, factor uint8) {
	if len(values) < 2 {
		alpConvertScalar(values, integers, accepted, exponent, factor)
		return
	}
	alpConvertAnalyzeNEON(values, integers, accepted, exponent, factor)
}

// alpConvertAnalyzeNEON also reduces extrema and exceptions in the same pass.
func alpConvertAnalyzeNEON(values []float64, integers []int64, accepted []uint64, exponent, factor uint8) (lo, hi int64, exceptions int) {
	power := archsimd.BroadcastFloat64x2(alpPowers[exponent])
	fraction := archsimd.BroadcastFloat64x2(alpFractions[factor])
	inverse := archsimd.BroadcastFloat64x2(alpFractions[exponent])
	magic := archsimd.BroadcastFloat64x2(6755399441055744.0)
	lower := archsimd.BroadcastInt64x2(alpLower[factor])
	upper := archsimd.BroadcastInt64x2(alpUpper[factor])
	domainLo := archsimd.BroadcastFloat64x2(-0x1p63)
	domainHi := archsimd.BroadcastFloat64x2(0x1p63)
	low := archsimd.BroadcastInt64x2(1<<63 - 1)
	high := archsimd.BroadcastInt64x2(-1 << 63)
	missing := archsimd.BroadcastUint64x2(0)
	one := archsimd.BroadcastUint64x2(1)
	i := 0
	for ; i+2 <= len(values); i += 2 {
		original := archsimd.LoadFloat64x2(values[i:])
		y := original.Mul(power).Mul(fraction).Add(magic).Sub(magic)
		valid := y.GreaterEqual(domainLo).And(y.Less(domainHi))
		q := y.Masked(valid).ConvertToInt64()
		valid = valid.And(q.GreaterEqual(lower)).And(q.LessEqual(upper))
		product := q.ToBits()
		if factor != 0 {
			product = alpMultiplyNEON(product, uint64(alpFactors[factor]))
		}
		restored := product.BitsToInt64().ConvertToFloat64().Mul(inverse)
		valid = valid.And(restored.ToBits().Equal(original.ToBits()))
		q.Store(integers[i:])
		valid.ToInt64x2().ToBits().Store(accepted[i:])
		low = q.IfElse(q.Less(low).And(valid), low)
		high = q.IfElse(q.Greater(high).And(valid), high)
		missing = missing.Add(one.Masked(valid.Not()))
	}
	lo, hi, exceptions = alpConvertAnalyzeScalar(values[i:], integers[i:], accepted[i:], exponent, factor)
	var lows, highs [2]int64
	var counts [2]uint64
	low.Store(lows[:])
	high.Store(highs[:])
	missing.Store(counts[:])
	for lane := range lows {
		lo = min(lo, lows[lane])
		hi = max(hi, highs[lane])
		exceptions += int(counts[lane])
	}
	return lo, hi, exceptions
}

func alpConvertNative(values []float64, integers []int64, accepted []uint64, exponent, factor uint8) {
	alpConvertNEON(values, integers, accepted, exponent, factor)
}

// alpDecodeIntegersNEON reconstructs independent unsigned integers in vectors.
func alpDecodeIntegersNEON(dst, words []uint64, width int, base uint64) {
	mask := archsimd.BroadcastUint64x2(alpMask(width))
	frame := archsimd.BroadcastUint64x2(base)
	for row := 0; row*alpLanes < len(dst); row++ {
		bit := row * width
		word, shift := (bit/64)*alpLanes, uint64(bit%64)
		for lane := 0; lane < alpLanes && row*alpLanes+lane < len(dst); lane += 2 {
			x := archsimd.LoadUint64x2(words[word+lane:]).ShiftAllRight(shift)
			if shift+uint64(width) > 64 {
				x = x.Or(archsimd.LoadUint64x2(words[word+lane+alpLanes:]).ShiftAllLeft(64 - shift))
			}
			out := dst[row*alpLanes+lane:]
			v := x.And(mask).Add(frame)
			if len(out) >= 2 {
				v.Store(out)
			} else {
				v.StorePart(out)
			}
		}
	}
}

func alpDecodeIntegersNative(dst, words []uint64, width int, base uint64) {
	alpDecodeIntegersNEON(dst, words, width, base)
}

func alpRestoreIntegersNEON(previous, delta, encoded []uint64) {
	one := archsimd.BroadcastUint64x2(1)
	zero := archsimd.BroadcastUint64x2(0)
	i := 0
	for ; i+2 <= len(encoded); i += 2 {
		z := archsimd.LoadUint64x2(encoded[i:])
		d := z.ShiftAllRight(1).Xor(zero.Sub(z.And(one)))
		d = d.Add(archsimd.LoadUint64x2(delta[i:]))
		p := archsimd.LoadUint64x2(previous[i:]).Add(d)
		d.Store(delta[i:])
		p.Store(previous[i:])
	}
	alpRestoreIntegersScalar(previous[i:], delta[i:], encoded[i:])
}

func alpRestoreIntegersNative(previous, delta, encoded []uint64) {
	alpRestoreIntegersNEON(previous, delta, encoded)
}

func alpConvertAnalyzeNative(values []float64, integers []int64, accepted []uint64, exponent, factor uint8) (int64, int64, int) {
	return alpConvertAnalyzeNEON(values, integers, accepted, exponent, factor)
}
