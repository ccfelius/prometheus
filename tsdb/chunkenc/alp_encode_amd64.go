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

// alpPredictIntegersAVX2 predicts independent fields modulo 2^64 in 4 lanes.
func alpPredictIntegersAVX2(previous, delta, fields []uint64) {
	i := 0
	for ; i+4 <= len(fields); i += 4 {
		v := archsimd.LoadUint64x4(fields[i:])
		d := v.Sub(archsimd.LoadUint64x4(previous[i:]))
		dd := d.Sub(archsimd.LoadUint64x4(delta[i:]))
		v.Store(previous[i:])
		d.Store(delta[i:])
		dd.ShiftAllLeft(1).Xor(dd.BitsToInt64().ShiftAllRight(63).ToBits()).Store(fields[i:])
	}
	alpPredictIntegersScalar(previous[i:], delta[i:], fields[i:])
}

func alpReduceAVX2(integers []int64, accepted []uint64) (lo, hi int64, exceptions int) {
	low := archsimd.BroadcastInt64x4(1<<63 - 1)
	high := archsimd.BroadcastInt64x4(-1 << 63)
	zero := archsimd.BroadcastUint64x4(0)
	one := archsimd.BroadcastUint64x4(1)
	counts := zero
	i := 0
	for ; i+4 <= len(integers); i += 4 {
		q := archsimd.LoadInt64x4(integers[i:])
		invalid := archsimd.LoadUint64x4(accepted[i:]).Equal(zero)
		low = q.IfElse(q.Less(low).And(archsimd.LoadUint64x4(accepted[i:]).Greater(zero)), low)
		high = q.IfElse(q.Greater(high).And(archsimd.LoadUint64x4(accepted[i:]).Greater(zero)), high)
		counts = counts.Add(one.Masked(invalid))
	}
	var lows, highs [4]int64
	var missing [4]uint64
	low.Store(lows[:])
	high.Store(highs[:])
	counts.Store(missing[:])
	lo, hi, exceptions = alpReduceScalar(integers[i:], accepted[i:])
	for j := range lows {
		lo = min(lo, lows[j])
		hi = max(hi, highs[j])
		exceptions += int(missing[j])
	}
	return lo, hi, exceptions
}

// alpPredictIntegersAVX512 predicts independent fields modulo 2^64 in 8 lanes.
func alpPredictIntegersAVX512(previous, delta, fields []uint64) {
	i := 0
	for ; i+8 <= len(fields); i += 8 {
		v := archsimd.LoadUint64x8(fields[i:])
		d := v.Sub(archsimd.LoadUint64x8(previous[i:]))
		dd := d.Sub(archsimd.LoadUint64x8(delta[i:]))
		v.Store(previous[i:])
		d.Store(delta[i:])
		dd.ShiftAllLeft(1).Xor(dd.BitsToInt64().ShiftAllRight(63).ToBits()).Store(fields[i:])
	}
	alpPredictIntegersScalar(previous[i:], delta[i:], fields[i:])
}

func alpReduceAVX512(integers []int64, accepted []uint64) (lo, hi int64, exceptions int) {
	low := archsimd.BroadcastInt64x8(1<<63 - 1)
	high := archsimd.BroadcastInt64x8(-1 << 63)
	zero := archsimd.BroadcastUint64x8(0)
	one := archsimd.BroadcastUint64x8(1)
	counts := zero
	i := 0
	for ; i+8 <= len(integers); i += 8 {
		q := archsimd.LoadInt64x8(integers[i:])
		invalid := archsimd.LoadUint64x8(accepted[i:]).Equal(zero)
		low = q.IfElse(q.Less(low).And(archsimd.LoadUint64x8(accepted[i:]).Greater(zero)), low)
		high = q.IfElse(q.Greater(high).And(archsimd.LoadUint64x8(accepted[i:]).Greater(zero)), high)
		counts = counts.Add(one.Masked(invalid))
	}
	var lows, highs [8]int64
	var missing [8]uint64
	low.Store(lows[:])
	high.Store(highs[:])
	counts.Store(missing[:])
	lo, hi, exceptions = alpReduceScalar(integers[i:], accepted[i:])
	for j := range lows {
		lo = min(lo, lows[j])
		hi = max(hi, highs[j])
		exceptions += int(missing[j])
	}
	return lo, hi, exceptions
}

func alpPredictIntegersNative(previous, delta, fields []uint64) {
	switch alpBackend {
	case "avx512":
		alpPredictIntegersAVX512(previous, delta, fields)
	case "avx2":
		alpPredictIntegersAVX2(previous, delta, fields)
	default:
		alpPredictIntegersScalar(previous, delta, fields)
	}
}

func alpReduceNative(integers []int64, accepted []uint64) (int64, int64, int) {
	switch alpBackend {
	case "avx512":
		return alpReduceAVX512(integers, accepted)
	case "avx2":
		return alpReduceAVX2(integers, accepted)
	default:
		return alpReduceScalar(integers, accepted)
	}
}

// alpPackWordsAVX2 packs independent lanes in complete rows, then handles a
// partial row without reading beyond the caller's value slice.
func alpPackWordsAVX2(words, values []uint64, width int) {
	rows := len(values) / alpLanes
	for row := range rows {
		bit := row * width
		word, shift := bit/64*alpLanes, uint64(bit%64)
		for lane := 0; lane < alpLanes; lane += 4 {
			v := archsimd.LoadUint64x4(values[row*alpLanes+lane:])
			v.ShiftAllLeft(shift).Or(archsimd.LoadUint64x4(words[word+lane:])).Store(words[word+lane:])
			if shift+uint64(width) > 64 {
				v.ShiftAllRight(64 - shift).Or(archsimd.LoadUint64x4(words[word+lane+alpLanes:])).Store(words[word+lane+alpLanes:])
			}
		}
	}
	for i := rows * alpLanes; i < len(values); i++ {
		bit := rows * width
		word, shift := bit/64*alpLanes+i%alpLanes, uint(bit%64)
		words[word] |= values[i] << shift
		if shift+uint(width) > 64 {
			words[word+alpLanes] |= values[i] >> (64 - shift)
		}
	}
}

// alpPackWordsAVX512 packs independent lanes in complete rows, then handles a
// partial row without reading beyond the caller's value slice.
func alpPackWordsAVX512(words, values []uint64, width int) {
	rows := len(values) / alpLanes
	for row := range rows {
		bit := row * width
		word, shift := bit/64*alpLanes, uint64(bit%64)
		for lane := 0; lane < alpLanes; lane += 8 {
			v := archsimd.LoadUint64x8(values[row*alpLanes+lane:])
			v.ShiftAllLeft(shift).Or(archsimd.LoadUint64x8(words[word+lane:])).Store(words[word+lane:])
			if shift+uint64(width) > 64 {
				v.ShiftAllRight(64 - shift).Or(archsimd.LoadUint64x8(words[word+lane+alpLanes:])).Store(words[word+lane+alpLanes:])
			}
		}
	}
	for i := rows * alpLanes; i < len(values); i++ {
		bit := rows * width
		word, shift := bit/64*alpLanes+i%alpLanes, uint(bit%64)
		words[word] |= values[i] << shift
		if shift+uint(width) > 64 {
			words[word+alpLanes] |= values[i] >> (64 - shift)
		}
	}
}

func alpPackWordsNative(words, values []uint64, width int) {
	switch alpBackend {
	case "avx512":
		alpPackWordsAVX512(words, values, width)
	case "avx2":
		alpPackWordsAVX2(words, values, width)
	default:
		alpPackWordsScalar(words, values, width)
	}
}

// alpRestoreTemporalAVX2 reconstructs independent fields before applying the
// exact decimal transform. Invalid arithmetic is reported before output escapes.
func alpRestoreTemporalAVX2(dst []float64, encoded []uint64, stride int, exponent, factor uint8) bool {
	if stride < 4 {
		return alpRestoreTemporalScalar(dst, encoded, stride, exponent, factor)
	}
	zero := archsimd.BroadcastUint64x4(0)
	one := archsimd.BroadcastUint64x4(1)
	low := archsimd.BroadcastInt64x4(1<<63 - 1)
	high := archsimd.BroadcastInt64x4(-1 << 63)
	scale := archsimd.BroadcastFloat64x4(alpFractions[exponent])
	for phase := range 2 {
		start, end := 0, stride
		if phase == 1 {
			start, end = stride, len(dst)
		}
		i := start
		for ; i+4 <= end; i += 4 {
			z := archsimd.LoadUint64x4(encoded[i:])
			q := z.ShiftAllRight(1).Xor(zero.Sub(z.And(one)))
			if phase == 1 {
				q = q.Add(archsimd.LoadUint64x4(encoded[i-stride:]))
			}
			q.Store(encoded[i:])
			signed := q.BitsToInt64()
			low = signed.IfElse(signed.Less(low), low)
			high = signed.IfElse(signed.Greater(high), high)
			alpInt64ToFloat64AVX2(alpMultiplyAVX2(q, uint64(alpFactors[factor]))).Mul(scale).Store(dst[i:])
		}
		for ; i < end; i++ {
			z := encoded[i]
			q := z>>1 ^ (0 - (z & 1))
			if phase == 1 {
				q += encoded[i-stride]
			}
			encoded[i] = q
			x := int64(q)
			if x < alpLower[factor] || x > alpUpper[factor] {
				return false
			}
			dst[i] = float64(x*alpFactors[factor]) * alpFractions[exponent]
		}
	}
	var lows, highs [4]int64
	low.Store(lows[:])
	high.Store(highs[:])
	for i := range lows {
		if lows[i] < alpLower[factor] || highs[i] > alpUpper[factor] {
			return false
		}
	}
	return true
}

// alpRestoreTemporalAVX512 reconstructs independent fields before applying the
// exact decimal transform. Invalid arithmetic is reported before output escapes.
func alpRestoreTemporalAVX512(dst []float64, encoded []uint64, stride int, exponent, factor uint8) bool {
	if stride < 8 {
		return alpRestoreTemporalScalar(dst, encoded, stride, exponent, factor)
	}
	zero := archsimd.BroadcastUint64x8(0)
	one := archsimd.BroadcastUint64x8(1)
	low := archsimd.BroadcastInt64x8(1<<63 - 1)
	high := archsimd.BroadcastInt64x8(-1 << 63)
	scale := archsimd.BroadcastFloat64x8(alpFractions[exponent])
	for phase := range 2 {
		start, end := 0, stride
		if phase == 1 {
			start, end = stride, len(dst)
		}
		i := start
		for ; i+8 <= end; i += 8 {
			z := archsimd.LoadUint64x8(encoded[i:])
			q := z.ShiftAllRight(1).Xor(zero.Sub(z.And(one)))
			if phase == 1 {
				q = q.Add(archsimd.LoadUint64x8(encoded[i-stride:]))
			}
			q.Store(encoded[i:])
			signed := q.BitsToInt64()
			low = signed.IfElse(signed.Less(low), low)
			high = signed.IfElse(signed.Greater(high), high)
			q.BitsToInt64().Mul(archsimd.BroadcastInt64x8(alpFactors[factor])).ConvertToFloat64().Mul(scale).Store(dst[i:])
		}
		for ; i < end; i++ {
			z := encoded[i]
			q := z>>1 ^ (0 - (z & 1))
			if phase == 1 {
				q += encoded[i-stride]
			}
			encoded[i] = q
			x := int64(q)
			if x < alpLower[factor] || x > alpUpper[factor] {
				return false
			}
			dst[i] = float64(x*alpFactors[factor]) * alpFractions[exponent]
		}
	}
	var lows, highs [8]int64
	low.Store(lows[:])
	high.Store(highs[:])
	for i := range lows {
		if lows[i] < alpLower[factor] || highs[i] > alpUpper[factor] {
			return false
		}
	}
	return true
}

func alpRestoreTemporalNative(dst []float64, encoded []uint64, stride int, exponent, factor uint8) bool {
	switch alpBackend {
	case "avx512":
		return alpRestoreTemporalAVX512(dst, encoded, stride, exponent, factor)
	case "avx2":
		return alpRestoreTemporalAVX2(dst, encoded, stride, exponent, factor)
	default:
		return alpRestoreTemporalScalar(dst, encoded, stride, exponent, factor)
	}
}
