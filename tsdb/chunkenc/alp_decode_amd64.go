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

// alpConvertAVX512 performs exact candidate conversion in 8 lanes.
func alpConvertAVX512(values []float64, integers []int64, accepted []uint64, exponent, factor uint8) {
	alpConvertAnalyzeAVX512(values, integers, accepted, exponent, factor)
}

// alpConvertAnalyzeAVX512 also reduces extrema and exceptions in the same pass.
func alpConvertAnalyzeAVX512(values []float64, integers []int64, accepted []uint64, exponent, factor uint8) (lo, hi int64, exceptions int) {
	power := archsimd.BroadcastFloat64x8(alpPowers[exponent])
	fraction := archsimd.BroadcastFloat64x8(alpFractions[factor])
	inverse := archsimd.BroadcastFloat64x8(alpFractions[exponent])
	magic := archsimd.BroadcastFloat64x8(6755399441055744.0)
	lower := archsimd.BroadcastInt64x8(alpLower[factor])
	upper := archsimd.BroadcastInt64x8(alpUpper[factor])
	domainLo := archsimd.BroadcastFloat64x8(-0x1p63)
	domainHi := archsimd.BroadcastFloat64x8(0x1p63)
	low := archsimd.BroadcastInt64x8(1<<63 - 1)
	high := archsimd.BroadcastInt64x8(-1 << 63)
	missing := archsimd.BroadcastUint64x8(0)
	one := archsimd.BroadcastUint64x8(1)
	i := 0
	for ; i+8 <= len(values); i += 8 {
		original := archsimd.LoadFloat64x8(values[i:])
		y := original.Mul(power).Mul(fraction).Add(magic).Sub(magic)
		valid := y.GreaterEqual(domainLo).And(y.Less(domainHi))
		q := y.Masked(valid).ConvertToInt64()
		valid = valid.And(q.GreaterEqual(lower)).And(q.LessEqual(upper))
		restored := q.Mul(archsimd.BroadcastInt64x8(alpFactors[factor])).ConvertToFloat64().Mul(inverse)
		valid = valid.And(restored.ToBits().Equal(original.ToBits()))
		q.Store(integers[i:])
		valid.ToInt64x8().ToBits().Store(accepted[i:])
		low = q.IfElse(q.Less(low).And(valid), low)
		high = q.IfElse(q.Greater(high).And(valid), high)
		missing = missing.Add(one.Masked(valid.ToInt64x8().Equal(archsimd.BroadcastInt64x8(0))))
	}
	lo, hi, exceptions = alpConvertAnalyzeScalar(values[i:], integers[i:], accepted[i:], exponent, factor)
	var lows, highs [8]int64
	var counts [8]uint64
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

// alpConvertAVX2 performs exact candidate conversion in 4 lanes.
func alpConvertAVX2(values []float64, integers []int64, accepted []uint64, exponent, factor uint8) {
	power := archsimd.BroadcastFloat64x4(alpPowers[exponent])
	fraction := archsimd.BroadcastFloat64x4(alpFractions[factor])
	inverse := archsimd.BroadcastFloat64x4(alpFractions[exponent])
	magic := archsimd.BroadcastFloat64x4(6755399441055744.0)
	lower := archsimd.BroadcastInt64x4(alpLower[factor])
	upper := archsimd.BroadcastInt64x4(alpUpper[factor])
	domainLo := archsimd.BroadcastFloat64x4(-0x1p63)
	domainHi := archsimd.BroadcastFloat64x4(0x1p63)
	i := 0
	for ; i+4 <= len(values); i += 4 {
		original := archsimd.LoadFloat64x4(values[i:])
		y := original.Mul(power).Mul(fraction).Add(magic).Sub(magic)
		valid := y.GreaterEqual(domainLo).And(y.Less(domainHi))
		// The bit construction is exact in [-2^51, 2^51). Other lanes
		// use the full-range scalar oracle; AVX2 has no packed float64/int64 cast.
		domain := y.GreaterEqual(archsimd.BroadcastFloat64x4(-0x1p51)).And(y.Less(archsimd.BroadcastFloat64x4(0x1p51)))
		q := y.Masked(domain).Add(magic).ToBits().Sub(archsimd.BroadcastUint64x4(0x4338000000000000)).BitsToInt64()
		valid = valid.And(domain)
		valid = valid.And(q.GreaterEqual(lower)).And(q.LessEqual(upper))
		product := q.ToBits()
		if factor != 0 {
			product = alpMultiplyAVX2(product, uint64(alpFactors[factor]))
		}
		restored := alpInt64ToFloat64AVX2(product).Mul(inverse)
		valid = valid.And(restored.ToBits().Equal(original.ToBits()))
		q.Store(integers[i:])
		valid.ToInt64x4().ToBits().Store(accepted[i:])
		var domainWords [4]int64
		domain.ToInt64x4().Store(domainWords[:])
		for lane, mask := range domainWords {
			if mask != 0 {
				continue
			}
			n, ok := alpEncodeNumber(values[i+lane], exponent, factor)
			integers[i+lane], accepted[i+lane] = n, 0
			if ok {
				accepted[i+lane] = 1
			}
		}
	}
	alpConvertScalar(values[i:], integers[i:], accepted[i:], exponent, factor)
}

func alpConvertNative(values []float64, integers []int64, accepted []uint64, exponent, factor uint8) {
	switch alpBackend {
	case "avx512":
		alpConvertAVX512(values, integers, accepted, exponent, factor)
	case "avx2":
		alpConvertAVX2(values, integers, accepted, exponent, factor)
	default:
		alpConvertScalar(values, integers, accepted, exponent, factor)
	}
}

// alpDecodeIntegersAVX2 reconstructs independent unsigned integers in vectors.
func alpDecodeIntegersAVX2(dst, words []uint64, width int, base uint64) {
	mask := archsimd.BroadcastUint64x4(alpMask(width))
	frame := archsimd.BroadcastUint64x4(base)
	for row := 0; row*alpLanes < len(dst); row++ {
		bit := row * width
		word, shift := (bit/64)*alpLanes, uint64(bit%64)
		for lane := 0; lane < alpLanes && row*alpLanes+lane < len(dst); lane += 4 {
			x := archsimd.LoadUint64x4(words[word+lane:]).ShiftAllRight(shift)
			if shift+uint64(width) > 64 {
				x = x.Or(archsimd.LoadUint64x4(words[word+lane+alpLanes:]).ShiftAllLeft(64 - shift))
			}
			out := dst[row*alpLanes+lane:]
			v := x.And(mask).Add(frame)
			if len(out) >= 4 {
				v.Store(out)
			} else {
				v.StorePart(out)
			}
		}
	}
}

// alpDecodeIntegersAVX512 reconstructs independent unsigned integers in vectors.
func alpDecodeIntegersAVX512(dst, words []uint64, width int, base uint64) {
	mask := archsimd.BroadcastUint64x8(alpMask(width))
	frame := archsimd.BroadcastUint64x8(base)
	for row := 0; row*alpLanes < len(dst); row++ {
		bit := row * width
		word, shift := (bit/64)*alpLanes, uint64(bit%64)
		for lane := 0; lane < alpLanes && row*alpLanes+lane < len(dst); lane += 8 {
			x := archsimd.LoadUint64x8(words[word+lane:]).ShiftAllRight(shift)
			if shift+uint64(width) > 64 {
				x = x.Or(archsimd.LoadUint64x8(words[word+lane+alpLanes:]).ShiftAllLeft(64 - shift))
			}
			out := dst[row*alpLanes+lane:]
			v := x.And(mask).Add(frame)
			if len(out) >= 8 {
				v.Store(out)
			} else {
				v.StorePart(out)
			}
		}
	}
}

func alpDecodeIntegersNative(dst, words []uint64, width int, base uint64) {
	switch alpBackend {
	case "avx512":
		alpDecodeIntegersAVX512(dst, words, width, base)
	case "avx2":
		alpDecodeIntegersAVX2(dst, words, width, base)
	default:
		for i := range dst {
			dst[i] = base + alpUnpackAt(words, i, width)
		}
	}
}

func alpRestoreIntegersAVX2(previous, delta, encoded []uint64) {
	one := archsimd.BroadcastUint64x4(1)
	zero := archsimd.BroadcastUint64x4(0)
	i := 0
	for ; i+4 <= len(encoded); i += 4 {
		z := archsimd.LoadUint64x4(encoded[i:])
		d := z.ShiftAllRight(1).Xor(zero.Sub(z.And(one)))
		d = d.Add(archsimd.LoadUint64x4(delta[i:]))
		p := archsimd.LoadUint64x4(previous[i:]).Add(d)
		d.Store(delta[i:])
		p.Store(previous[i:])
	}
	alpRestoreIntegersScalar(previous[i:], delta[i:], encoded[i:])
}

func alpRestoreIntegersAVX512(previous, delta, encoded []uint64) {
	one := archsimd.BroadcastUint64x8(1)
	zero := archsimd.BroadcastUint64x8(0)
	i := 0
	for ; i+8 <= len(encoded); i += 8 {
		z := archsimd.LoadUint64x8(encoded[i:])
		d := z.ShiftAllRight(1).Xor(zero.Sub(z.And(one)))
		d = d.Add(archsimd.LoadUint64x8(delta[i:]))
		p := archsimd.LoadUint64x8(previous[i:]).Add(d)
		d.Store(delta[i:])
		p.Store(previous[i:])
	}
	alpRestoreIntegersScalar(previous[i:], delta[i:], encoded[i:])
}

func alpRestoreIntegersNative(previous, delta, encoded []uint64) {
	switch alpBackend {
	case "avx512":
		alpRestoreIntegersAVX512(previous, delta, encoded)
	case "avx2":
		alpRestoreIntegersAVX2(previous, delta, encoded)
	default:
		alpRestoreIntegersScalar(previous, delta, encoded)
	}
}

func alpConvertAnalyzeNative(values []float64, integers []int64, accepted []uint64, exponent, factor uint8) (int64, int64, int) {
	switch alpBackend {
	case "avx512":
		return alpConvertAnalyzeAVX512(values, integers, accepted, exponent, factor)
	case "avx2":
		alpConvertAVX2(values, integers, accepted, exponent, factor)
		return alpReduceAVX2(integers, accepted)
	default:
		return alpConvertAnalyzeScalar(values, integers, accepted, exponent, factor)
	}
}
