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
)

// ALPEncoder reuses bounded temporary storage while converting finalized chunks.
// The zero value is ready to use. It retains no source or published output buffers.
// It is not safe for concurrent use. Call ResetSeries at every series boundary.
// Returned chunks own their bytes and remain valid after reuse of the encoder.
type ALPEncoder struct {
	values, counts alpEncodeState
	skipFloats     uint8
	skipHistograms uint8
	histogramKey   uint64
	samples        [alpBlockSize]alpSample
	hist           alpHistogramWorkspace
}

// Recode converts a finalized XOR or histogram chunk into an independent ALP
// chunk. The returned chunk owns its bytes; the source remains unchanged.
func (e *ALPEncoder) Recode(source Chunk) (Chunk, error) {
	switch source.Encoding() {
	case EncXOR, EncXOR2:
		return e.recodeFloats(source, source.Iterator(nil), 0)
	case EncHistogram, EncHistogramST, EncFloatHistogram, EncFloatHistogramST:
		enc := EncALPHistogram
		if source.Encoding() == EncFloatHistogram || source.Encoding() == EncFloatHistogramST {
			enc = EncALPFloatHistogram
		}
		b, err := alpEncodeHistogramsWithWorkspace(source, enc, &e.counts, alpHistogramMetadataVersion, &e.hist)
		if err != nil {
			return nil, err
		}
		return &ALPHistogramChunk{encoding: enc, version: alpHistogramMetadataVersion, encoded: b}, nil
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
	// A recently successful decimal plan is already a positive series hint.
	// The writer still validates all values and complete output size below.
	if !e.values.valid || e.values.uses >= 15 {
		for n < len(sample) && it.Next() != ValNone {
			ts, v := it.At()
			sample[n] = v
			e.samples[n] = alpSample{st: it.AtST(), t: ts, v: v}
			n++
		}
		if err := it.Err(); err != nil {
			return nil, err
		}
		plan, promising := alpPromisingDecimal(sample[:n])
		if n == 0 || !promising {
			e.skipFloats = 7
			return nil, nil
		}
		e.values = alpEncodeState{exponent: plan.exponent, factor: plan.factor, valid: true}
	}
	c, err := e.recodeFloats(source, it, n)
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
func alpPromisingDecimal(values []float64) (alpDecimalPlan, bool) {
	var plan alpDecimalPlan
	promising := func(exponent, factor uint8) bool {
		p := alpAnalyze(values, exponent, factor, 1)
		plan = p
		return p.exceptions < len(values) && alpDecimalCost(p, len(values)) <= 14+2*len(values)
	}
	for exponent := uint8(0); exponent <= 6; exponent++ {
		if promising(exponent, 0) {
			return plan, true
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
				return plan, true
			}
		}
	}
	return alpDecimalPlan{}, false
}

// ResetSeries forgets prediction and rejection hints while retaining bounded
// scratch buffers. It must be called before converting a different series.
func (e *ALPEncoder) ResetSeries() {
	e.values, e.counts, e.hist.sums = alpEncodeState{}, alpEncodeState{}, alpEncodeState{}
	e.skipFloats, e.skipHistograms, e.histogramKey = 0, 0, 0
}

func (e *ALPEncoder) recodeFloats(source Chunk, it Iterator, used int) (Chunk, error) {
	n := source.NumSamples()
	if n < 0 || n > math.MaxUint16 {
		return nil, errInvalidALP
	}
	dst := make([]byte, alpHeaderSize, max(alpHeaderSize, len(source.Bytes())))
	binary.BigEndian.PutUint16(dst, uint16(n))
	dst[2] = alpVersion
	count := used
	for it.Next() != ValNone {
		ts, v := it.At()
		e.samples[used] = alpSample{st: it.AtST(), t: ts, v: v}
		used++
		count++
		if used == alpBlockSize {
			dst = alpEncodeSamplesWithState(dst, e.samples[:used], &e.values)
			used = 0
		}
	}
	if err := it.Err(); err != nil {
		return nil, err
	}
	if count != n {
		return nil, errInvalidALP
	}
	if used != 0 {
		dst = alpEncodeSamplesWithState(dst, e.samples[:used], &e.values)
	}
	c := NewALPChunk()
	c.Reset(dst)
	c.validated = true
	return c, nil
}

// RecodeHistogramV2 converts a finalized integer histogram using compact numeric
// vectors and reuses temporary storage. The result owns its bytes.
func (e *ALPEncoder) RecodeHistogramV2(source Chunk) (Chunk, error) {
	if source.Encoding() != EncHistogram && source.Encoding() != EncHistogramST {
		return nil, errInvalidALP
	}
	b, err := alpEncodeHistogramsWithWorkspace(source, EncALPHistogram, &e.counts, alpHistogramCompactVersion, &e.hist)
	if err != nil {
		return nil, err
	}
	return &ALPHistogramChunk{encoding: EncALPHistogram, version: alpHistogramCompactVersion, encoded: b}, nil
}

// RecodeHistogramV3 converts a finalized integer or float histogram using packed
// reset hints and, for integer histograms, a packed first sample. Readers must
// support histogram version 3. The result owns its bytes; scratch is reused.
func (e *ALPEncoder) RecodeHistogramV3(source Chunk) (Chunk, error) {
	enc := EncALPHistogram
	switch source.Encoding() {
	case EncHistogram, EncHistogramST:
	case EncFloatHistogram, EncFloatHistogramST:
		enc = EncALPFloatHistogram
	default:
		return nil, errInvalidALP
	}
	b, err := alpEncodeHistogramsWithWorkspace(source, enc, &e.counts, alpHistogramMetadataVersion, &e.hist)
	if err != nil {
		return nil, err
	}
	return &ALPHistogramChunk{encoding: enc, version: alpHistogramMetadataVersion, encoded: b}, nil
}

// RecodeHistogramIfSmaller considers a finalized histogram for adaptive v3
// storage. A nil result retains the source. Rejected layouts are retried every
// eighth chunk; layout or reset changes retry immediately. A prefix estimate
// may miss savings, but accepted chunks are exact and save at least 5% of bytes.
// The encoder retains no source buffers and returned chunks own their bytes.
func (e *ALPEncoder) RecodeHistogramIfSmaller(source Chunk) (Chunk, error) {
	enc := EncALPHistogram
	switch source.Encoding() {
	case EncHistogram, EncHistogramST:
	case EncFloatHistogram, EncFloatHistogramST:
		enc = EncALPFloatHistogram
	default:
		return nil, errInvalidALP
	}
	n := source.NumSamples()
	if n < 0 {
		return nil, errInvalidALP
	}
	if n > MaxSamplesPerALPHistogramChunk || len(source.Bytes()) <= n+32 {
		return nil, nil
	}
	defer e.hist.releaseLarge()
	it := source.Iterator(nil)
	typ := it.Next()
	if typ == ValNone {
		return nil, it.Err()
	}
	// A layout hash is only a selection hint, never part of decoding correctness.
	key := uint64(source.Encoding()) + uint64(n)<<8
	add := func(v uint64) { key = (key ^ v) * 1099511628211 }
	var header CounterResetHeader
	if c, ok := source.(interface{ GetCounterResetHeader() CounterResetHeader }); ok {
		header = c.GetCounterResetHeader()
	}
	add(uint64(header))
	switch typ {
	case ValHistogram:
		_, e.hist.integerHistogram = it.AtHistogram(e.hist.integerHistogram)
		h := e.hist.integerHistogram
		add(uint64(h.Schema))
		add(math.Float64bits(h.ZeroThreshold))
		for _, span := range h.PositiveSpans {
			add(uint64(span.Offset))
			add(uint64(span.Length))
		}
		add(math.MaxUint64)
		for _, span := range h.NegativeSpans {
			add(uint64(span.Offset))
			add(uint64(span.Length))
		}
		for _, v := range h.CustomValues {
			add(math.Float64bits(v))
		}
	case ValFloatHistogram:
		_, e.hist.floatHistogram = it.AtFloatHistogram(e.hist.floatHistogram)
		h := e.hist.floatHistogram
		add(uint64(h.Schema))
		add(math.Float64bits(h.ZeroThreshold))
		for _, span := range h.PositiveSpans {
			add(uint64(span.Offset))
			add(uint64(span.Length))
		}
		add(math.MaxUint64)
		for _, span := range h.NegativeSpans {
			add(uint64(span.Offset))
			add(uint64(span.Length))
		}
		for _, v := range h.CustomValues {
			add(math.Float64bits(v))
		}
	default:
		return nil, errInvalidALP
	}
	if key != e.histogramKey {
		e.skipHistograms = 0
		e.histogramKey = key
	}
	if e.skipHistograms != 0 {
		e.skipHistograms--
		return nil, nil
	}
	budget := len(source.Bytes())/100*95 + len(source.Bytes())%100*95/100
	data, err := alpEncodeHistogramCandidate(source, enc, &e.counts, alpHistogramMetadataVersion, &e.hist, budget, it, typ)
	if err != nil {
		return nil, err
	}
	if data == nil || len(data) > budget {
		e.skipHistograms = 7
		return nil, nil
	}
	return &ALPHistogramChunk{encoding: enc, version: alpHistogramMetadataVersion, encoded: data}, nil
}

// RecodeHistogramV4 converts a finalized histogram using sparse integer
// exceptions, adaptive integer prediction, and exact temporal decimal counts.
// Version 4 requires upgraded readers and is an explicit experimental choice.
// The result owns its bytes and the encoder retains only bounded scratch.
func (e *ALPEncoder) RecodeHistogramV4(source Chunk) (Chunk, error) {
	enc := EncALPHistogram
	switch source.Encoding() {
	case EncHistogram, EncHistogramST:
	case EncFloatHistogram, EncFloatHistogramST:
		enc = EncALPFloatHistogram
	default:
		return nil, errInvalidALP
	}
	data, err := alpEncodeHistogramsWithWorkspace(source, enc, &e.counts, alpHistogramPredictiveVersion, &e.hist)
	if err != nil {
		return nil, err
	}
	return &ALPHistogramChunk{encoding: enc, version: alpHistogramPredictiveVersion, encoded: data}, nil
}
