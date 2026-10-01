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

import (
	"encoding/binary"
	"math"
	"math/rand"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestALPHistogramV4Integers(t *testing.T) {
	random := rand.New(rand.NewSource(402))
	for _, n := range []int{1, 2, 15, 16, 17, 127, 128, 129, 511, 1024} {
		for width := 0; width <= 64; width++ {
			values := make([]uint64, n)
			for i := range values {
				values[i] = random.Uint64() & alpMask(width)
			}
			values[n/2] = math.MaxUint64
			encoded := alpEncodePatchedIntegers(nil, values)
			require.LessOrEqual(t, len(encoded), len(alpEncodeIntegers(nil, values)))
			decoded := make([]uint64, n)
			var scratch alpDecodeScratch
			require.NoError(t, alpDecodePatchedIntegers(decoded, encoded, &scratch))
			require.Equal(t, values, decoded)
		}
	}
	values := make([]uint64, 128)
	for i := range values {
		values[i] = uint64(i % 2)
	}
	values[7], values[89] = math.MaxUint64, math.MaxUint64-1
	encoded := alpEncodePatchedIntegers(nil, values)
	require.Equal(t, byte(2), encoded[0])
	require.Equal(t, byte(1), encoded[1])
	patches := 12 + alpPackedSize(len(values), int(encoded[1]))
	for name, corrupt := range map[string]func([]byte){
		"width":         func(b []byte) { b[1] = 65 },
		"count":         func(b []byte) { binary.LittleEndian.PutUint16(b[10:], 129) },
		"out of bounds": func(b []byte) { binary.LittleEndian.PutUint16(b[patches:], 128) },
		"duplicate":     func(b []byte) { copy(b[patches+10:patches+12], b[patches:patches+2]) },
		"overflow":      func(b []byte) { binary.LittleEndian.PutUint64(b[2:], math.MaxUint64) },
	} {
		t.Run(name, func(t *testing.T) {
			data := slices.Clone(encoded)
			corrupt(data)
			var scratch alpDecodeScratch
			require.Error(t, alpDecodePatchedIntegers(make([]uint64, len(values)), data, &scratch))
		})
	}
	for end := range len(encoded) {
		var scratch alpDecodeScratch
		require.Error(t, alpDecodePatchedIntegers(make([]uint64, len(values)), encoded[:end], &scratch))
	}
}

func TestALPHistogramV4TemporalFloats(t *testing.T) {
	for _, fields := range []int{2, 3, 10, 130, 1023} {
		values := make([]float64, 1024)
		for i := range values {
			values[i] = float64(i/fields*3 + i%fields)
		}
		var state alpEncodeState
		encoded := alpEncodeTemporalFloats(nil, values, &state, fields)
		var scratch alpDecodeScratch
		decoded := make([]float64, len(values))
		require.NoError(t, alpDecodeTemporalFloats(decoded, encoded, &scratch, fields))
		require.Equal(t, values, decoded)
		if fields == 10 {
			require.Equal(t, byte(alpHistogramTemporal), encoded[0])
			for name, corrupt := range map[string]func([]byte){
				"exponent": func(b []byte) { b[1] = 19 },
				"factor":   func(b []byte) { b[2] = 19 },
				"stride":   func(b []byte) { binary.LittleEndian.PutUint16(b[3:], 11) },
				"length":   func(b []byte) { binary.LittleEndian.PutUint32(b[5:], math.MaxUint32) },
			} {
				t.Run(name, func(t *testing.T) {
					data := slices.Clone(encoded)
					corrupt(data)
					require.Error(t, alpDecodeTemporalFloats(decoded, data, &scratch, fields))
				})
			}
			for end := range len(encoded) {
				require.Error(t, alpDecodeTemporalFloats(decoded, encoded[:end], &scratch, fields))
			}
		}
		for _, special := range []float64{math.Copysign(0, -1), math.Inf(1), math.Float64frombits(0x7ff8000000000042)} {
			values[17] = special
			state = alpEncodeState{}
			encoded = alpEncodeTemporalFloats(nil, values, &state, fields)
			require.NoError(t, alpDecodeTemporalFloats(decoded, encoded, &scratch, fields))
			for i, v := range values {
				require.Equal(t, math.Float64bits(v), math.Float64bits(decoded[i]))
			}
		}
	}
}
