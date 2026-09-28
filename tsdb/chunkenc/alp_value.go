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
	"errors"
	"math"
	"math/bits"
	"slices"
)

const (
	alpBlockSize    = 128
	alpMaxBlockSize = 1024
	alpLanes        = 16
)

const (
	alpRaw = iota + 1
	alpConstant
	alpDecimal
	alpRD
)

var errInvalidALP = errors.New("invalid ALP chunk")

// These binary64 tables fix the rounding contract across architectures. The
// transform follows ALP (Afroozeh, Kuffo, Boncz, DOI 10.1145/3626717).
var (
	alpPowers    = [...]float64{1, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18}
	alpFractions = [...]float64{1, 1e-1, 1e-2, 1e-3, 1e-4, 1e-5, 1e-6, 1e-7, 1e-8, 1e-9, 1e-10, 1e-11, 1e-12, 1e-13, 1e-14, 1e-15, 1e-16, 1e-17, 1e-18}
	alpFactors   = [...]int64{1, 10, 100, 1000, 10000, 100000, 1000000, 10000000, 100000000, 1000000000, 10000000000, 100000000000, 1000000000000, 10000000000000, 100000000000000, 1000000000000000, 10000000000000000, 100000000000000000, 1000000000000000000}
)

type alpDecimalPlan struct {
	exponent, factor, width uint8
	base                    int64
	exceptions              int
}

func alpEncodeNumber(x float64, exponent, factor uint8) (int64, bool) {
	// Explicit conversions retain the two binary64 rounding points.
	y := float64(x * alpPowers[exponent])
	y = float64(y * alpFractions[factor])
	const magic = 6755399441055744.0
	y = float64(y + magic)
	y = float64(y - magic)
	if math.IsNaN(y) || y < -0x1p63 || y >= 0x1p63 {
		return 0, false
	}
	q := int64(y)
	p := alpFactors[factor]
	if q < math.MinInt64/p || q > math.MaxInt64/p {
		return 0, false
	}
	decoded := float64(q*p) * alpFractions[exponent]
	return q, math.Float64bits(decoded) == math.Float64bits(x)
}

func alpAnalyze(values []float64, exponent, factor uint8, stride int) alpDecimalPlan {
	p := alpDecimalPlan{exponent: exponent, factor: factor, base: math.MaxInt64}
	hi := int64(math.MinInt64)
	for i := 0; i < len(values); i += stride {
		q, ok := alpEncodeNumber(values[i], exponent, factor)
		if !ok {
			p.exceptions++
			continue
		}
		p.base = min(p.base, q)
		hi = max(hi, q)
	}
	if p.base > hi {
		p.base = 0
		return p
	}
	p.width = uint8(bits.Len64(uint64(hi) - uint64(p.base)))
	return p
}

func alpDecimalCost(p alpDecimalPlan, n int) int {
	return 14 + alpPackedSize(n, int(p.width)) + 10*p.exceptions
}

// alpEncodeValues appends one self-contained value block. The wire layout is a
// compact-tail adaptation of ALP's 16-lane FastLanes layout, not CWI's file format.
func alpEncodeValues(dst []byte, values []float64) []byte {
	n := len(values)
	first := math.Float64bits(values[0])
	constant := true
	for _, v := range values[1:] {
		if math.Float64bits(v) != first {
			constant = false
			break
		}
	}
	if constant {
		dst = append(dst, alpConstant)
		return binary.LittleEndian.AppendUint64(dst, first)
	}
	// Search a bounded sample, then verify the five best pairs on every value.
	stride := max(1, (n+31)/32)
	var candidates [5]alpDecimalPlan
	var costs [5]int
	for i := range costs {
		costs[i] = math.MaxInt
	}
	for exponent := range uint8(len(alpPowers)) {
		for factor := uint8(0); factor <= exponent; factor++ {
			p := alpAnalyze(values, exponent, factor, stride)
			sampled := (n + stride - 1) / stride
			p.exceptions = (p.exceptions*n + sampled - 1) / sampled
			cost := alpDecimalCost(p, n)
			for j := range candidates {
				if cost < costs[j] {
					copy(candidates[j+1:], candidates[j:])
					copy(costs[j+1:], costs[j:])
					candidates[j], costs[j] = p, cost
					break
				}
			}
		}
	}
	bestSize := 1 + 8*n
	mode := byte(alpRaw)
	var best alpDecimalPlan
	for _, candidate := range candidates {
		p := alpAnalyze(values, candidate.exponent, candidate.factor, 1)
		cost := alpDecimalCost(p, n)
		if p.exceptions != n && cost < bestSize {
			best, bestSize, mode = p, cost, alpDecimal
		}
	}
	var rd alpRDPlan
	if bestSize > 6*n {
		rd = alpAnalyzeRD(values)
		if rd.size(n) < bestSize {
			mode = alpRD
		}
	}
	switch mode {
	case alpDecimal:
		dst = append(dst, alpDecimal, best.width, best.exponent, best.factor)
		dst = binary.LittleEndian.AppendUint64(dst, uint64(best.base))
		dst = binary.LittleEndian.AppendUint16(dst, uint16(best.exceptions))
		var residuals [alpMaxBlockSize]uint64
		var exceptions [alpMaxBlockSize]bool
		for i, v := range values {
			q, ok := alpEncodeNumber(v, best.exponent, best.factor)
			if ok {
				residuals[i] = uint64(q) - uint64(best.base)
			} else {
				exceptions[i] = true
			}
		}
		dst = alpPack(dst, residuals[:n], int(best.width))
		for i, exception := range exceptions[:n] {
			if exception {
				dst = binary.LittleEndian.AppendUint16(dst, uint16(i))
				dst = binary.LittleEndian.AppendUint64(dst, math.Float64bits(values[i]))
			}
		}
		return dst
	case alpRD:
		return alpEncodeRD(dst, values, rd)
	default:
		dst = append(dst, alpRaw)
		for _, v := range values {
			dst = binary.LittleEndian.AppendUint64(dst, math.Float64bits(v))
		}
		return dst
	}
}

type alpRDPlan struct {
	split, width, count uint8
	dict                [8]uint16
	exceptions          int
}

func (p alpRDPlan) size(n int) int {
	return 6 + 2*int(p.count) + alpPackedSize(n, int(p.split)) + alpPackedSize(n, int(p.width)) + 4*p.exceptions
}

func alpAnalyzeRD(values []float64) alpRDPlan {
	var best alpRDPlan
	bestSize := math.MaxInt
	var high [alpMaxBlockSize]uint16
	for split := uint8(48); split < 64; split++ {
		for i, v := range values {
			high[i] = uint16(math.Float64bits(v) >> split)
		}
		slices.Sort(high[:len(values)])
		var dict [8]uint16
		var counts [8]int
		for i := 0; i < len(values); {
			j := i + 1
			for j < len(values) && high[j] == high[i] {
				j++
			}
			for k := range dict {
				if j-i > counts[k] {
					copy(dict[k+1:], dict[k:])
					copy(counts[k+1:], counts[k:])
					dict[k], counts[k] = high[i], j-i
					break
				}
			}
			i = j
		}
		p := alpRDPlan{split: split, dict: dict, exceptions: len(values)}
		for i, count := range counts {
			if count == 0 {
				break
			}
			p.count = uint8(i + 1)
			p.width = uint8(max(1, bits.Len(uint(i))))
			p.exceptions -= count
			if size := p.size(len(values)); size < bestSize {
				best, bestSize = p, size
			}
		}
	}
	return best
}

func alpEncodeRD(dst []byte, values []float64, p alpRDPlan) []byte {
	dst = append(dst, alpRD, p.split, p.count, p.width)
	dst = binary.LittleEndian.AppendUint16(dst, uint16(p.exceptions))
	for _, entry := range p.dict[:p.count] {
		dst = binary.LittleEndian.AppendUint16(dst, entry)
	}
	var low, indices [alpMaxBlockSize]uint64
	var exceptions [alpMaxBlockSize]bool
	for i, v := range values {
		u := math.Float64bits(v)
		low[i] = u & alpMask(int(p.split))
		index := slices.Index(p.dict[:p.count], uint16(u>>p.split))
		if index < 0 {
			exceptions[i] = true
		} else {
			indices[i] = uint64(index)
		}
	}
	dst = alpPack(dst, low[:len(values)], int(p.split))
	dst = alpPack(dst, indices[:len(values)], int(p.width))
	for i, exception := range exceptions[:len(values)] {
		if exception {
			dst = binary.LittleEndian.AppendUint16(dst, uint16(i))
			dst = binary.LittleEndian.AppendUint16(dst, uint16(math.Float64bits(values[i])>>p.split))
		}
	}
	return dst
}

type alpDecodeScratch struct {
	words   []uint64
	indices []uint64
}

// alpDecodeValues borrows src for the call and writes only into caller-owned
// dst and scratch. Callers must not expose dst when an error is returned.
func alpDecodeValues(dst []float64, src []byte, scratch *alpDecodeScratch, kernel alpKernel) error {
	n := len(dst)
	if n == 0 || n > alpMaxBlockSize || len(src) == 0 {
		return errInvalidALP
	}
	switch src[0] {
	case alpRaw:
		if len(src) != 1+8*n {
			return errInvalidALP
		}
		for i := range dst {
			dst[i] = math.Float64frombits(binary.LittleEndian.Uint64(src[1+8*i:]))
		}
	case alpConstant:
		if len(src) != 9 {
			return errInvalidALP
		}
		v := math.Float64frombits(binary.LittleEndian.Uint64(src[1:]))
		for i := range dst {
			dst[i] = v
		}
	case alpDecimal:
		if len(src) < 14 || src[1] > 64 || src[2] > 18 || src[3] > src[2] {
			return errInvalidALP
		}
		width, exponent, factor := int(src[1]), src[2], src[3]
		base := int64(binary.LittleEndian.Uint64(src[4:]))
		exceptions := int(binary.LittleEndian.Uint16(src[12:]))
		packed := alpPackedSize(n, width)
		if exceptions > n || len(src) != 14+packed+10*exceptions {
			return errInvalidALP
		}
		scratch.words = alpUnpackWords(scratch.words, src[14:14+packed], n, width)
		p := alpFactors[factor]
		lo, hi := int64(math.MinInt64)/p, int64(math.MaxInt64)/p
		// Most blocks prove arithmetic safety from metadata alone. Unusual or
		// corrupt ranges take the checked scalar path before results are exposed.
		if base >= lo && base <= hi && alpMask(width) <= uint64(hi)-uint64(base) {
			kernel(dst, scratch.words, width, base, factor, exponent)
		} else {
			for i := range dst {
				q := int64(uint64(base) + alpUnpackAt(scratch.words, i, width))
				if q < base || q < lo || q > hi {
					return errInvalidALP
				}
				dst[i] = float64(q*p) * alpFractions[exponent]
			}
		}
		previous := -1
		for pos := 14 + packed; pos < len(src); pos += 10 {
			i := int(binary.LittleEndian.Uint16(src[pos:]))
			if i <= previous || i >= n {
				return errInvalidALP
			}
			dst[i] = math.Float64frombits(binary.LittleEndian.Uint64(src[pos+2:]))
			previous = i
		}
	case alpRD:
		return alpDecodeRD(dst, src, scratch)
	default:
		return errInvalidALP
	}
	return nil
}

func alpDecodeRD(dst []float64, src []byte, scratch *alpDecodeScratch) error {
	if len(src) < 6 || src[1] < 48 || src[1] > 63 || src[2] == 0 || src[2] > 8 || src[3] < 1 || src[3] > 3 {
		return errInvalidALP
	}
	split, count, width := int(src[1]), int(src[2]), int(src[3])
	if count > 1<<width {
		return errInvalidALP
	}
	exceptions := int(binary.LittleEndian.Uint16(src[4:]))
	lowSize, indexSize := alpPackedSize(len(dst), split), alpPackedSize(len(dst), width)
	start := 6 + count*2
	if exceptions > len(dst) || len(src) != start+lowSize+indexSize+4*exceptions {
		return errInvalidALP
	}
	var dict [8]uint64
	for i := range count {
		dict[i] = uint64(binary.LittleEndian.Uint16(src[6+2*i:]))
		if dict[i] > alpMask(64-split) {
			return errInvalidALP
		}
	}
	scratch.words = alpUnpackWords(scratch.words, src[start:start+lowSize], len(dst), split)
	start += lowSize
	scratch.indices = alpUnpackWords(scratch.indices, src[start:start+indexSize], len(dst), width)
	for i := range dst {
		index := alpUnpackAt(scratch.indices, i, width)
		if index >= uint64(count) {
			return errInvalidALP
		}
		dst[i] = math.Float64frombits(dict[index]<<split | alpUnpackAt(scratch.words, i, split))
	}
	previous := -1
	for pos := start + indexSize; pos < len(src); pos += 4 {
		i := int(binary.LittleEndian.Uint16(src[pos:]))
		hi := uint64(binary.LittleEndian.Uint16(src[pos+2:]))
		if i <= previous || i >= len(dst) || hi > alpMask(64-split) {
			return errInvalidALP
		}
		dst[i] = math.Float64frombits(hi<<split | (math.Float64bits(dst[i]) & alpMask(split)))
		previous = i
	}
	return nil
}
