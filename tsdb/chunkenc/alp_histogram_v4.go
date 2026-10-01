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
	"math/bits"
)

// alpEncodePatchedIntegers retains the ordinary integer representation unless
// replacing a few wide residuals with exact exceptions makes the block smaller.
func alpEncodePatchedIntegers(dst []byte, values []uint64) []byte {
	lo, hi := values[0], values[0]
	for _, v := range values {
		lo = min(lo, v)
		hi = max(hi, v)
	}
	width := bits.Len64(hi - lo)
	bestSize := min(1+8*len(values), 10+alpPackedSize(len(values), width))
	var counts [65]int
	for _, v := range values {
		counts[bits.Len64(v-lo)]++
	}
	missing, bestWidth, bestMissing := len(values), -1, 0
	for w, count := range &counts {
		missing -= count
		if count == 0 || missing == 0 {
			continue
		}
		cost := 12 + alpPackedSize(len(values), w) + 10*missing
		if cost < bestSize {
			bestSize, bestWidth, bestMissing = cost, w, missing
		}
	}
	if bestWidth < 0 {
		return alpEncodeIntegers(dst, values)
	}
	dst = append(dst, 2, byte(bestWidth))
	dst = binary.LittleEndian.AppendUint64(dst, lo)
	dst = binary.LittleEndian.AppendUint16(dst, uint16(bestMissing))
	mask := alpMask(bestWidth)
	var residuals [alpMaxBlockSize]uint64
	for i, v := range values {
		if v-lo <= mask {
			residuals[i] = v - lo
		}
	}
	dst = alpPack(dst, residuals[:len(values)], bestWidth)
	for i, v := range values {
		if v-lo > mask {
			dst = binary.LittleEndian.AppendUint16(dst, uint16(i))
			dst = binary.LittleEndian.AppendUint64(dst, v)
		}
	}
	return dst
}

func alpValidIntegerBlock(src []byte, n int, patched bool) bool {
	if n <= 0 || n > alpMaxBlockSize {
		return false
	}
	if len(src) == 1+8*n && src[0] == 0 {
		return true
	}
	if len(src) >= 10 && src[0] == 1 && src[1] <= 64 {
		return len(src) == 10+alpPackedSize(n, int(src[1]))
	}
	if !patched || len(src) < 12 || src[0] != 2 || src[1] > 64 {
		return false
	}
	count := int(binary.LittleEndian.Uint16(src[10:]))
	return count <= n && len(src) == 12+alpPackedSize(n, int(src[1]))+10*count
}

func alpDecodePatchedIntegers(dst []uint64, src []byte, scratch *alpDecodeScratch) error {
	if len(dst) == 0 || len(dst) > alpMaxBlockSize {
		return errInvalidALP
	}
	if len(src) == 0 || src[0] != 2 {
		return alpDecodeIntegers(dst, src, scratch)
	}
	if !alpValidIntegerBlock(src, len(dst), true) {
		return errInvalidALP
	}
	width, base := int(src[1]), binary.LittleEndian.Uint64(src[2:])
	packed := alpPackedSize(len(dst), width)
	scratch.words = alpUnpackWords(scratch.words, src[12:12+packed], len(dst), width)
	if alpMask(width) <= math.MaxUint64-base {
		alpDecodeIntegersNative(dst, scratch.words, width, base)
	} else {
		for i := range dst {
			v := alpUnpackAt(scratch.words, i, width)
			if v > math.MaxUint64-base {
				return errInvalidALP
			}
			dst[i] = base + v
		}
	}
	previous := -1
	for pos := 12 + packed; pos < len(src); pos += 10 {
		index := int(binary.LittleEndian.Uint16(src[pos:]))
		if index <= previous || index >= len(dst) {
			return errInvalidALP
		}
		dst[index] = binary.LittleEndian.Uint64(src[pos+2:])
		previous = index
	}
	return nil
}

// alpHistogramTemporal is only emitted inside histogram version 4. It stores
// exactly representable decimal integers, not rounded floating differences.
const alpHistogramTemporal = 5

func alpEncodeTemporalFloats(dst []byte, values []float64, state *alpEncodeState, fields int) []byte {
	start := len(dst)
	dst = alpEncodeValuesWithState(dst, values, state)
	baselineEnd := len(dst)
	if !state.valid || fields >= len(values) {
		return dst
	}
	var q [alpMaxBlockSize]int64
	var valid, encoded [alpMaxBlockSize]uint64
	_, _, exceptions := alpConvertAnalyzeNative(values, q[:len(values)], valid[:len(values)], state.exponent, state.factor)
	// Ordinary ALP retains exact exceptions and unusual bit patterns.
	if exceptions != 0 {
		return dst
	}
	for i, x := range q[:len(values)] {
		d := x
		if i >= fields {
			d = int64(uint64(x) - uint64(q[i-fields]))
		}
		encoded[i] = uint64(d<<1) ^ uint64(d>>63)
	}
	dst = append(dst, alpHistogramTemporal, state.exponent, state.factor)
	dst = binary.LittleEndian.AppendUint16(dst, uint16(fields))
	lengthOffset := len(dst)
	dst = append(dst, 0, 0, 0, 0)
	first := len(dst)
	dst = alpEncodePatchedIntegers(dst, encoded[:fields])
	binary.LittleEndian.PutUint32(dst[lengthOffset:], uint32(len(dst)-first))
	dst = alpEncodePatchedIntegers(dst, encoded[fields:len(values)])
	if len(dst)-baselineEnd < baselineEnd-start {
		n := copy(dst[start:], dst[baselineEnd:])
		return dst[:start+n]
	}
	return dst[:baselineEnd]
}

func alpDecodeTemporalFloats(dst []float64, src []byte, scratch *alpDecodeScratch, fields int) error {
	if len(src) == 0 || src[0] != alpHistogramTemporal {
		return alpDecodeValues(dst, src, scratch, alpDecodeNative)
	}
	if len(src) < 9 || src[1] > 18 || src[2] > src[1] {
		return errInvalidALP
	}
	stride := int(binary.LittleEndian.Uint16(src[3:]))
	first := uint64(binary.LittleEndian.Uint32(src[5:]))
	if stride != fields || stride < 2 || stride >= len(dst) || first > uint64(len(src)-9) {
		return errInvalidALP
	}
	var encoded [alpMaxBlockSize]uint64
	if err := alpDecodePatchedIntegers(encoded[:stride], src[9:9+int(first)], scratch); err != nil {
		return err
	}
	if err := alpDecodePatchedIntegers(encoded[stride:len(dst)], src[9+int(first):], scratch); err != nil {
		return err
	}
	if !alpRestoreTemporalNative(dst, encoded[:len(dst)], stride, src[1], src[2]) {
		return errInvalidALP
	}
	return nil
}

func alpRestoreTemporalScalar(dst []float64, encoded []uint64, stride int, exponent, factor uint8) bool {
	for i, z := range encoded[:len(dst)] {
		q := (z >> 1) ^ (0 - (z & 1))
		if i >= stride {
			q += encoded[i-stride]
		}
		encoded[i] = q
		x := int64(q)
		if x < alpLower[factor] || x > alpUpper[factor] {
			return false
		}
		dst[i] = float64(x*alpFactors[factor]) * alpFractions[exponent]
	}
	return true
}
