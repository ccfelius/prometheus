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

//go:build !go1.27 || !goexperiment.simd || (!amd64 && !arm64)

package chunkenc

var alpDecodeNative alpKernel = alpDecodeScalar

const alpBackend = "scalar"

func alpConvertNative(values []float64, integers []int64, accepted []uint64, exponent, factor uint8) {
	alpConvertScalar(values, integers, accepted, exponent, factor)
}

func alpDecodeIntegersNative(dst, words []uint64, width int, base uint64) {
	for i := range dst {
		dst[i] = base + alpUnpackAt(words, i, width)
	}
}

func alpRestoreIntegersNative(previous, delta, encoded []uint64) {
	alpRestoreIntegersScalar(previous, delta, encoded)
}

func alpPredictIntegersNative(previous, delta, fields []uint64) {
	alpPredictIntegersScalar(previous, delta, fields)
}

func alpReduceNative(integers []int64, accepted []uint64) (int64, int64, int) {
	return alpReduceScalar(integers, accepted)
}
