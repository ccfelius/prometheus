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

// alpPredictIntegersNEON predicts independent fields modulo 2^64 in 2 lanes.
func alpPredictIntegersNEON(previous, delta, fields []uint64) {
	i := 0
	for ; i+2 <= len(fields); i += 2 {
		v := archsimd.LoadUint64x2(fields[i:])
		d := v.Sub(archsimd.LoadUint64x2(previous[i:]))
		dd := d.Sub(archsimd.LoadUint64x2(delta[i:]))
		v.Store(previous[i:])
		d.Store(delta[i:])
		dd.ShiftAllLeft(1).Xor(dd.BitsToInt64().ShiftAllRight(63).ToBits()).Store(fields[i:])
	}
	alpPredictIntegersScalar(previous[i:], delta[i:], fields[i:])
}

func alpReduceNEON(integers []int64, accepted []uint64) (lo, hi int64, exceptions int) {
	low := archsimd.BroadcastInt64x2(1<<63 - 1)
	high := archsimd.BroadcastInt64x2(-1 << 63)
	zero := archsimd.BroadcastUint64x2(0)
	one := archsimd.BroadcastUint64x2(1)
	counts := zero
	i := 0
	for ; i+2 <= len(integers); i += 2 {
		q := archsimd.LoadInt64x2(integers[i:])
		invalid := archsimd.LoadUint64x2(accepted[i:]).Equal(zero)
		low = q.IfElse(q.Less(low).And(invalid.Not()), low)
		high = q.IfElse(q.Greater(high).And(invalid.Not()), high)
		counts = counts.Add(one.Masked(invalid))
	}
	var lows, highs [2]int64
	var missing [2]uint64
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
	alpPredictIntegersNEON(previous, delta, fields)
}

func alpReduceNative(integers []int64, accepted []uint64) (int64, int64, int) {
	return alpReduceNEON(integers, accepted)
}

// alpPackWordsNEON packs independent lanes in complete rows, then handles a
// partial row without reading beyond the caller's value slice.
func alpPackWordsNEON(words, values []uint64, width int) {
	rows := len(values) / alpLanes
	for row := range rows {
		bit := row * width
		word, shift := bit/64*alpLanes, uint64(bit%64)
		for lane := 0; lane < alpLanes; lane += 2 {
			v := archsimd.LoadUint64x2(values[row*alpLanes+lane:])
			v.ShiftAllLeft(shift).Or(archsimd.LoadUint64x2(words[word+lane:])).Store(words[word+lane:])
			if shift+uint64(width) > 64 {
				v.ShiftAllRight(64 - shift).Or(archsimd.LoadUint64x2(words[word+lane+alpLanes:])).Store(words[word+lane+alpLanes:])
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

func alpPackWordsNative(words, values []uint64, width int) { alpPackWordsNEON(words, values, width) }

// alpRestoreTemporalNEON reconstructs independent fields before applying the
// exact decimal transform. Invalid arithmetic is reported before output escapes.
func alpRestoreTemporalNEON(dst []float64, encoded []uint64, stride int, exponent, factor uint8) bool {
	if stride < 2 {
		return alpRestoreTemporalScalar(dst, encoded, stride, exponent, factor)
	}
	zero := archsimd.BroadcastUint64x2(0)
	one := archsimd.BroadcastUint64x2(1)
	low := archsimd.BroadcastInt64x2(1<<63 - 1)
	high := archsimd.BroadcastInt64x2(-1 << 63)
	scale := archsimd.BroadcastFloat64x2(alpFractions[exponent])
	for phase := range 2 {
		start, end := 0, stride
		if phase == 1 {
			start, end = stride, len(dst)
		}
		i := start
		for ; i+2 <= end; i += 2 {
			z := archsimd.LoadUint64x2(encoded[i:])
			q := z.ShiftAllRight(1).Xor(zero.Sub(z.And(one)))
			if phase == 1 {
				q = q.Add(archsimd.LoadUint64x2(encoded[i-stride:]))
			}
			q.Store(encoded[i:])
			signed := q.BitsToInt64()
			low = signed.IfElse(signed.Less(low), low)
			high = signed.IfElse(signed.Greater(high), high)
			alpMultiplyNEON(q, uint64(alpFactors[factor])).BitsToInt64().ConvertToFloat64().Mul(scale).Store(dst[i:])
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
	var lows, highs [2]int64
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
	return alpRestoreTemporalNEON(dst, encoded, stride, exponent, factor)
}
