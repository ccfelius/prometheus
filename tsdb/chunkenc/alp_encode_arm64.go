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

import (
	"math"
	"simd/archsimd"
)

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
	low := archsimd.BroadcastInt64x2(math.MaxInt64)
	high := archsimd.BroadcastInt64x2(math.MinInt64)
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
	return
}

func alpPredictIntegersNative(previous, delta, fields []uint64) {
	alpPredictIntegersNEON(previous, delta, fields)
}

func alpReduceNative(integers []int64, accepted []uint64) (int64, int64, int) {
	return alpReduceNEON(integers, accepted)
}
