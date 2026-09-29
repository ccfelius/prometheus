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

// ALPEncoder reuses small encoding hints across finalized chunks of one series.
// The zero value is ready to use. It retains no source or output buffers and is
// not safe for concurrent use. Use a new encoder at each series boundary.
type ALPEncoder struct {
	values, counts alpEncodeState
	skipFloats     uint8
}

// Recode converts a finalized XOR or histogram chunk into an independent ALP
// chunk. The returned chunk owns its bytes; the source remains unchanged.
func (e *ALPEncoder) Recode(source Chunk) (Chunk, error) {
	switch source.Encoding() {
	case EncXOR, EncXOR2:
		c := NewALPChunk()
		c.state = e.values
		c.pending = make([]alpSample, 0, min(source.NumSamples(), alpBlockSize))
		a, _ := c.Appender()
		it := source.Iterator(nil)
		for it.Next() != ValNone {
			ts, v := it.At()
			a.Append(it.AtST(), ts, v)
		}
		if err := it.Err(); err != nil {
			return nil, err
		}
		c.Bytes()
		e.values = c.state
		c.Compact()
		return c, nil
	case EncHistogram, EncHistogramST, EncFloatHistogram, EncFloatHistogramST:
		enc := EncALPHistogram
		if source.Encoding() == EncFloatHistogram || source.Encoding() == EncFloatHistogramST {
			enc = EncALPFloatHistogram
		}
		b, err := alpEncodeHistograms(source, enc, &e.counts, alpVersion)
		if err != nil {
			return nil, err
		}
		return &ALPHistogramChunk{encoding: enc, version: alpVersion, encoded: b}, nil
	default:
		return nil, errInvalidALP
	}
}

// RecodeFloatIfSmaller considers a finalized XOR/XOR2 chunk for adaptive storage.
// A nil result means retain the source. The encoder samples promising decimal
// scales before a full conversion and retries rejected series every eighth chunk.
// Sampling may miss compression opportunities; accepted output is still exact
// and must save at least 5% of complete bytes. Source ownership is unchanged.
func (e *ALPEncoder) RecodeFloatIfSmaller(source Chunk) (Chunk, error) {
	if source.Encoding() != EncXOR && source.Encoding() != EncXOR2 {
		return nil, errInvalidALP
	}
	if len(source.Bytes()) <= 40 {
		return nil, nil
	}
	if e.skipFloats != 0 {
		e.skipFloats--
		return nil, nil
	}
	var sample [16]float64
	it := source.Iterator(nil)
	n := 0
	for n < len(sample) && it.Next() != ValNone {
		_, sample[n] = it.At()
		n++
	}
	if err := it.Err(); err != nil {
		return nil, err
	}
	if n == 0 || !alpPromisingDecimal(sample[:n]) {
		e.skipFloats = 7
		return nil, nil
	}
	c, err := e.Recode(source)
	if err != nil {
		return nil, err
	}
	if len(c.Bytes())*100 > len(source.Bytes())*95 {
		e.skipFloats = 7
		return nil, nil
	}
	return c, nil
}

// alpPromisingDecimal bounds the sampling work, not the compression guarantee.
// The full writer validates every value and the caller checks complete bytes.
func alpPromisingDecimal(values []float64) bool {
	promising := func(exponent, factor uint8) bool {
		p := alpAnalyze(values, exponent, factor, 1)
		return p.exceptions < len(values) && alpDecimalCost(p, len(values)) <= 14+2*len(values)
	}
	for exponent := uint8(0); exponent <= 6; exponent++ {
		if promising(exponent, 0) {
			return true
		}
	}
	maximum := 0.0
	for _, v := range values {
		if !math.IsNaN(v) && !math.IsInf(v, 0) {
			maximum = max(maximum, math.Abs(v))
		}
	}
	exponent := uint8(18)
	for exponent > 0 && maximum >= float64(math.MaxInt64)*alpFractions[exponent] {
		exponent--
	}
	for offset := uint8(0); offset < 2 && offset <= exponent; offset++ {
		e := exponent - offset
		for digits := uint8(0); digits <= min(e, 6); digits++ {
			if promising(e, e-digits) {
				return true
			}
		}
	}
	return false
}
