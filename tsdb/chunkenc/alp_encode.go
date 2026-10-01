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

import "math"

func alpPredictIntegersScalar(previous, delta, fields []uint64) {
	for i, v := range fields {
		d := v - previous[i]
		dd := int64(d - delta[i])
		previous[i], delta[i] = v, d
		fields[i] = uint64(dd<<1) ^ uint64(dd>>63)
	}
}

func alpReduceScalar(integers []int64, accepted []uint64) (lo, hi int64, exceptions int) {
	lo, hi = math.MaxInt64, math.MinInt64
	for i, q := range integers {
		if accepted[i] == 0 {
			exceptions++
			continue
		}
		lo, hi = min(lo, q), max(hi, q)
	}
	return lo, hi, exceptions
}

func alpConvertAnalyzeScalar(values []float64, integers []int64, accepted []uint64, exponent, factor uint8) (lo, hi int64, exceptions int) {
	lo, hi = math.MaxInt64, math.MinInt64
	for i, v := range values {
		q, ok := alpEncodeNumber(v, exponent, factor)
		integers[i], accepted[i] = q, 0
		if !ok {
			exceptions++
			continue
		}
		accepted[i] = 1
		lo, hi = min(lo, q), max(hi, q)
	}
	return lo, hi, exceptions
}
