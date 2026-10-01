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
	"slices"

	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/value"
)

// MaxSamplesPerALPHistogramChunk is the limit of its mutable ST histogram codec.
const MaxSamplesPerALPHistogramChunk = histogramSTSampleCountMask

const (
	alpHistogramCompactVersion    = 2
	alpHistogramMetadataVersion   = 3
	alpHistogramPredictiveVersion = 4
)

// ALPHistogramChunk stores histogram sums and floating-point counts in ALP
// vectors. Integer counts use lossless integer frame-of-reference packing.
// The existing ST-capable histogram appender maintains mutable chunks, including
// layout recoding and counter-reset detection. Serialization builds immutable
// ALP streams, and persisted chunks decode those streams directly with SIMD.
type ALPHistogramChunk struct {
	encoding Encoding
	version  byte
	inner    Chunk
	encoded  []byte
	err      error
}

// NewALPHistogramChunk returns an empty integer-count histogram chunk.
func NewALPHistogramChunk() *ALPHistogramChunk {
	return &ALPHistogramChunk{encoding: EncALPHistogram, version: alpHistogramMetadataVersion, inner: NewHistogramSTChunk()}
}

// NewALPFloatHistogramChunk returns an empty floating-count histogram chunk.
func NewALPFloatHistogramChunk() *ALPHistogramChunk {
	return &ALPHistogramChunk{encoding: EncALPFloatHistogram, version: alpHistogramMetadataVersion, inner: NewFloatHistogramSTChunk()}
}

// RecodeToALPHistogram converts a finalized histogram chunk while retaining the
// counter-reset header, layout, values, and start timestamps.
func RecodeToALPHistogram(source Chunk) (Chunk, error) {
	var encoder ALPEncoder
	return encoder.Recode(source)
}

// RecodeToALPHistogramV2 converts a finalized integer histogram using 128-value
// integer vectors. Version 2 requires a reader that supports the compact format.
// The source is unchanged and the result owns its encoded bytes.
func RecodeToALPHistogramV2(source Chunk) (Chunk, error) {
	if source.Encoding() != EncHistogram && source.Encoding() != EncHistogramST {
		return nil, errInvalidALP
	}
	var state alpEncodeState
	b, err := alpEncodeHistograms(source, EncALPHistogram, &state, alpHistogramCompactVersion)
	if err != nil {
		return nil, err
	}
	return &ALPHistogramChunk{encoding: EncALPHistogram, version: alpHistogramCompactVersion, encoded: b}, nil
}

func alpValidHistogramVersion(version byte, enc Encoding) bool {
	return version == alpVersion || version == alpHistogramMetadataVersion || version == alpHistogramPredictiveVersion || version == alpHistogramCompactVersion && enc == EncALPHistogram
}

// Encoding returns the integer- or floating-count ALP histogram encoding.
func (c *ALPHistogramChunk) Encoding() Encoding { return c.encoding }

// GetCounterResetHeader returns the original chunk-level reset marker. As with
// legacy iterators, the first counter sample is exposed with an unknown hint.
func (c *ALPHistogramChunk) GetCounterResetHeader() CounterResetHeader {
	if c.inner != nil {
		return c.inner.(interface{ GetCounterResetHeader() CounterResetHeader }).GetCounterResetHeader()
	}
	if len(c.encoded) < 4 {
		return UnknownCounterReset
	}
	return CounterResetHeader(c.encoded[3])
}

// NumSamples returns the sample count without materializing ALP streams.
func (c *ALPHistogramChunk) NumSamples() int {
	if c.inner != nil {
		return c.inner.NumSamples()
	}
	if len(c.encoded) < 4 {
		return 0
	}
	return int(binary.BigEndian.Uint16(c.encoded))
}

// Reset borrows immutable encoded bytes. Reset(nil) releases retained buffers.
func (c *ALPHistogramChunk) Reset(b []byte) {
	c.inner, c.encoded, c.err = nil, slices.Clip(b), nil
	c.version = alpHistogramMetadataVersion
	if len(b) >= 4 {
		c.version = b[2]
	}
	if len(b) != 0 && (len(b) < 4 || !alpValidHistogramVersion(b[2], c.encoding) || b[3]&^CounterResetHeaderMask != 0 || binary.BigEndian.Uint16(b) > MaxSamplesPerALPHistogramChunk) {
		c.err = errInvalidALP
	}
}

// Bytes returns an immutable serialization, cached until the next append.
func (c *ALPHistogramChunk) Bytes() []byte {
	if c.encoded != nil {
		return c.encoded
	}
	if c.inner == nil || c.inner.NumSamples() == 0 {
		return []byte{0, 0, c.version, 0}
	}
	var state alpEncodeState
	c.encoded, c.err = alpEncodeHistograms(c.inner, c.encoding, &state, c.version)
	if c.err != nil {
		panic(c.err)
	} // The mutable source is trusted appender output.
	return c.encoded
}

// Compact serializes the chunk and releases its mutable append representation.
func (c *ALPHistogramChunk) Compact() { c.encoded = c.Bytes(); c.inner = nil }

// HistogramChunkSize estimates the chunk-cutting size without ALP serialization.
// ALP histograms use their mutable histogram representation for this soft target.
func HistogramChunkSize(c Chunk) int {
	if a, ok := c.(*ALPHistogramChunk); ok && a.inner != nil {
		return len(a.inner.Bytes())
	}
	return len(c.Bytes())
}

// Appender restores mutable histogram state when necessary. Callers serialize
// creation and mutation; existing iterators retain independent snapshots.
func (c *ALPHistogramChunk) Appender() (Appender, error) {
	if c.err != nil {
		return nil, c.err
	}
	if c.inner == nil {
		var inner Chunk = NewHistogramSTChunk()
		if c.encoding == EncALPFloatHistogram {
			inner = NewFloatHistogramSTChunk()
		}
		a, err := inner.Appender()
		if err != nil {
			return nil, err
		}
		it := c.Iterator(nil)
		for typ := it.Next(); typ != ValNone; typ = it.Next() {
			if typ == ValHistogram {
				ts, h := it.AtHistogram(nil)
				_, _, a, err = a.AppendHistogram(nil, it.AtST(), ts, h, true)
			} else {
				ts, h := it.AtFloatHistogram(nil)
				_, _, a, err = a.AppendFloatHistogram(nil, it.AtST(), ts, h, true)
			}
			if err != nil {
				return nil, err
			}
		}
		if err := it.Err(); err != nil {
			return nil, err
		}
		a.(interface{ setCounterResetHeader(CounterResetHeader) }).setCounterResetHeader(c.GetCounterResetHeader())
		c.inner = inner
	}
	a, err := c.inner.Appender()
	return &alpHistogramAppender{c: c, inner: a}, err
}

type alpHistogramAppender struct {
	c     *ALPHistogramChunk
	inner Appender
}

func (*alpHistogramAppender) Append(int64, int64, float64) {
	panic("cannot append float to ALP histogram chunk")
}

func alpHistogramPrevious(prev Appender) Appender {
	if p, ok := prev.(*alpHistogramAppender); ok {
		return p.inner
	}
	return prev
}

func (a *alpHistogramAppender) appended(c Chunk, recoded bool, next Appender, err error) (Chunk, bool, Appender, error) {
	if err != nil {
		return nil, false, a, err
	}
	if c != nil {
		wrapped := &ALPHistogramChunk{encoding: a.c.encoding, version: a.c.version, inner: c}
		return wrapped, recoded, &alpHistogramAppender{c: wrapped, inner: next}, nil
	}
	a.c.encoded = nil
	a.inner = next
	return nil, false, a, nil
}

func (a *alpHistogramAppender) AppendHistogram(prev Appender, st, t int64, h *histogram.Histogram, only bool) (Chunk, bool, Appender, error) {
	return a.appended(a.inner.AppendHistogram(alpHistogramPrevious(prev), st, t, h, only))
}

func (a *alpHistogramAppender) AppendFloatHistogram(prev Appender, st, t int64, h *histogram.FloatHistogram, only bool) (Chunk, bool, Appender, error) {
	return a.appended(a.inner.AppendFloatHistogram(alpHistogramPrevious(prev), st, t, h, only))
}

// alpHistogramNumbers streams at most 1,024 counts at a time, independently of
// the number of samples or buckets in the chunk. Integer bits never pass through
// a floating-point conversion.
type alpHistogramNumbers struct {
	previous, delta           []uint64
	first                     bool
	compactFirst              bool
	predictive                bool
	predictor                 byte
	fields                    int
	src                       []byte
	floats                    []float64
	current                   []float64
	ints                      []uint64
	scratch                   alpDecodeScratch
	remaining, index, decoded int
	blockSize                 int
	integer                   bool
	err                       error
}

func (r *alpHistogramNumbers) refill() bool {
	if r.err != nil {
		return false
	}
	if r.remaining == 0 || len(r.src) < 4 {
		r.err = errInvalidALP
		return false
	}
	size := uint64(binary.LittleEndian.Uint32(r.src))
	if size > uint64(len(r.src)-4) {
		r.err = errInvalidALP
		return false
	}
	b := r.src[4 : 4+int(size)]
	r.src = r.src[4+int(size):]
	r.decoded = min(r.blockSize, r.remaining)
	r.remaining -= r.decoded
	r.index = 0
	if r.integer {
		r.ints = slices.Grow(r.ints[:0], r.decoded)[:r.decoded]
		if r.predictive {
			if len(b) == 0 || b[0] > 1 {
				r.err = errInvalidALP
				return false
			}
			r.predictor = b[0]
			b = b[1:]
			r.err = alpDecodePatchedIntegers(r.ints, b, &r.scratch)
		} else {
			r.err = alpDecodeIntegers(r.ints, b, &r.scratch)
		}
	} else {
		r.floats = slices.Grow(r.floats[:0], r.decoded)[:r.decoded]
		if r.predictive {
			r.err = alpDecodeTemporalFloats(r.floats, b, &r.scratch, r.fields)
		} else {
			r.err = alpDecodeValues(r.floats, b, &r.scratch, alpDecodeNative)
		}
	}
	return r.err == nil
}

func (r *alpHistogramNumbers) integerSample() []uint64 {
	if r.first {
		if r.compactFirst {
			for offset := 0; offset < len(r.previous); {
				if len(r.src) < 4 {
					r.err = errInvalidALP
					return r.previous
				}
				size := uint64(binary.LittleEndian.Uint32(r.src))
				if size > uint64(len(r.src)-4) {
					r.err = errInvalidALP
					return r.previous
				}
				take := min(r.blockSize, len(r.previous)-offset)
				if r.predictive {
					r.err = alpDecodePatchedIntegers(r.previous[offset:offset+take], r.src[4:4+int(size)], &r.scratch)
				} else {
					r.err = alpDecodeIntegers(r.previous[offset:offset+take], r.src[4:4+int(size)], &r.scratch)
				}
				if r.err != nil {
					return r.previous
				}
				r.src = r.src[4+int(size):]
				offset += take
			}

			if r.predictive {
				for i, z := range r.previous[2:] {
					r.previous[i+2] = z>>1 ^ (0 - (z & 1))
				}
			}
			r.first = false
			return r.previous
		}
		if len(r.src)/8 < len(r.previous) {
			r.err = errInvalidALP
			return r.previous
		}
		for i := range r.previous {
			r.previous[i] = binary.LittleEndian.Uint64(r.src[8*i:])
		}
		r.src = r.src[8*len(r.previous):]
		r.first = false
		return r.previous
	}
	for field := 0; field < len(r.previous); {
		if r.index == r.decoded && !r.refill() {
			return r.previous
		}
		take := min(len(r.previous)-field, r.decoded-r.index)
		if r.predictive && r.predictor == 1 {
			for j, z := range r.ints[r.index : r.index+take] {
				d := (z >> 1) ^ (0 - (z & 1))
				r.delta[field+j] = d
				r.previous[field+j] += d
			}
		} else {
			alpRestoreIntegersNative(r.previous[field:field+take], r.delta[field:field+take], r.ints[r.index:r.index+take])
		}
		r.index += take
		field += take
	}
	return r.previous
}

func (r *alpHistogramNumbers) readFloats(dst []float64) {
	for len(dst) > 0 {
		if r.index == r.decoded && !r.refill() {
			return
		}
		take := copy(dst, r.floats[r.index:r.decoded])
		r.index += take
		dst = dst[take:]
	}
}

// floatSample lends a view of a decoded vector until the next sample. A sample
// crossing a vector boundary is assembled in owned scratch before either vector
// can be overwritten. AtFloatHistogram copies the view into caller-owned output.
func (r *alpHistogramNumbers) floatSample(fields int) []float64 {
	if r.index == r.decoded && !r.refill() {
		r.current = slices.Grow(r.current[:0], fields)[:fields]
		return r.current
	}
	if fields <= r.decoded-r.index {
		values := r.floats[r.index : r.index+fields]
		r.index += fields
		return values
	}
	r.current = slices.Grow(r.current[:0], fields)[:fields]
	r.readFloats(r.current)
	return r.current
}

// alpRestoreIntegersScalar predicts independent fields modulo 2^64. Each input
// vector ends at a sample boundary, so no lane depends on another lane's result.
func alpRestoreIntegersScalar(previous, delta, encoded []uint64) {
	for i, z := range encoded {
		delta[i] += (z >> 1) ^ (0 - (z & 1))
		previous[i] += delta[i]
	}
}

func alpEncodeIntegers(dst []byte, values []uint64) []byte {
	lo, hi := values[0], values[0]
	for _, v := range values {
		lo = min(lo, v)
		hi = max(hi, v)
	}
	width := bits.Len64(hi - lo)
	if 10+alpPackedSize(len(values), width) >= 1+8*len(values) {
		dst = append(dst, 0)
		for _, v := range values {
			dst = binary.LittleEndian.AppendUint64(dst, v)
		}
		return dst
	}
	dst = append(dst, 1, byte(width))
	dst = binary.LittleEndian.AppendUint64(dst, lo)
	var residuals [alpMaxBlockSize]uint64
	for i, v := range values {
		residuals[i] = v - lo
	}
	return alpPack(dst, residuals[:len(values)], width)
}

func alpDecodeIntegers(dst []uint64, src []byte, scratch *alpDecodeScratch) error {
	if len(src) == 0 {
		return errInvalidALP
	}
	if src[0] == 0 {
		if len(src) != 1+8*len(dst) {
			return errInvalidALP
		}
		for i := range dst {
			dst[i] = binary.LittleEndian.Uint64(src[1+8*i:])
		}
		return nil
	}
	if len(src) < 10 || src[0] != 1 || src[1] > 64 {
		return errInvalidALP
	}
	w := int(src[1])
	base := binary.LittleEndian.Uint64(src[2:])
	if len(src) != 10+alpPackedSize(len(dst), w) {
		return errInvalidALP
	}
	scratch.words = alpUnpackWords(scratch.words, src[10:], len(dst), w)
	if alpMask(w) <= math.MaxUint64-base {
		alpDecodeIntegersNative(dst, scratch.words, w, base)
		return nil
	}
	for i := range dst {
		v := alpUnpackAt(scratch.words, i, w)
		if v > math.MaxUint64-base {
			return errInvalidALP
		}
		dst[i] = base + v
	}
	return nil
}

func alpAppendSpans(dst []byte, spans []histogram.Span) []byte {
	dst = binary.AppendUvarint(dst, uint64(len(spans)))
	for _, s := range spans {
		dst = binary.AppendVarint(dst, int64(s.Offset))
		dst = binary.AppendUvarint(dst, uint64(s.Length))
	}
	return dst
}

// alpHistogramWorkspace holds only owned scratch; no published bytes borrow it.
type alpHistogramWorkspace struct {
	hints, numeric, times   []byte
	previous, delta, fields []uint64
	samples                 [alpBlockSize]alpSample
	sums                    alpEncodeState
	integerHistogram        *histogram.Histogram
	floatHistogram          *histogram.FloatHistogram
}

// releaseLarge bounds retention after unusually wide or long histogram chunks.
func (w *alpHistogramWorkspace) releaseLarge() {
	const bytesLimit = 64 * 1024
	if cap(w.hints) > bytesLimit {
		w.hints = nil
	}
	if cap(w.numeric) > bytesLimit {
		w.numeric = nil
	}
	if cap(w.times) > bytesLimit {
		w.times = nil
	}
	if cap(w.previous) > bytesLimit/8 {
		w.previous = nil
	}
	if cap(w.fields) > bytesLimit/8 {
		w.fields = nil
	}
	if cap(w.delta) > bytesLimit/8 {
		w.delta = nil
	}
	if h := w.integerHistogram; h != nil && (cap(h.PositiveBuckets)+cap(h.NegativeBuckets)+cap(h.PositiveSpans)+cap(h.NegativeSpans)+cap(h.CustomValues)) > bytesLimit/8 {
		w.integerHistogram = nil
	}
	if h := w.floatHistogram; h != nil && (cap(h.PositiveBuckets)+cap(h.NegativeBuckets)+cap(h.PositiveSpans)+cap(h.NegativeSpans)+cap(h.CustomValues)) > bytesLimit/8 {
		w.floatHistogram = nil
	}
}

func alpEncodeHistograms(inner Chunk, enc Encoding, state *alpEncodeState, version byte) ([]byte, error) {
	var workspace alpHistogramWorkspace
	return alpEncodeHistogramsWithWorkspace(inner, enc, state, version, &workspace)
}

func alpEncodeHistogramsWithWorkspace(inner Chunk, enc Encoding, state *alpEncodeState, version byte, w *alpHistogramWorkspace) ([]byte, error) {
	return alpEncodeHistogramCandidate(inner, enc, state, version, w, 0, nil, ValNone)
}

// alpEncodeHistogramCandidate can abandon an adaptive trial without publishing
// partial output. A supplied iterator is positioned on first and is not retained.
func alpEncodeHistogramCandidate(inner Chunk, enc Encoding, state *alpEncodeState, version byte, w *alpHistogramWorkspace, budget int, it Iterator, first ValueType) ([]byte, error) {
	defer w.releaseLarge()
	n := inner.NumSamples()
	if !alpValidHistogramVersion(version, enc) {
		return nil, errInvalidALP
	}
	blockSize := alpMaxBlockSize
	if enc == EncALPHistogram && version >= alpHistogramCompactVersion {
		blockSize = alpBlockSize
	}
	if n < 0 || n > MaxSamplesPerALPHistogramChunk {
		return nil, errInvalidALP
	}
	b := inner.Bytes()
	flagOffset := histogramFlagPos
	if inner.Encoding() == EncHistogramST || inner.Encoding() == EncFloatHistogramST {
		flagOffset = 0
	}
	if len(b) <= flagOffset {
		return nil, errInvalidALP
	}
	header := b[flagOffset] & CounterResetHeaderMask
	if n == 0 {
		return []byte{0, 0, version, header}, nil
	}
	w.times = append(w.times[:0], byte(n>>8), byte(n), alpVersion)
	sumUsed := 0
	hints := slices.Grow(w.hints[:0], n)
	defer func() { w.hints = hints }()
	var layout histogram.FloatHistogram
	var positive, negative int
	// The source size is a useful reservation for a conversion candidate. Bound
	// transient reservation separately from retained scratch to avoid repeatedly
	// growing wide streams that releaseLarge intentionally drops after each call.
	numeric := slices.Grow(w.numeric[:0], min(len(b), 256*1024))
	defer func() { w.numeric = numeric }()
	var floats [alpMaxBlockSize]float64
	var integers, deltas [alpMaxBlockSize]uint64
	used := 0
	blocks, probeStart, probeValues := 0, 0, 0
	flush := func() {
		if used == 0 {
			return
		}
		start := len(numeric)
		numeric = append(numeric, 0, 0, 0, 0)
		if enc == EncALPHistogram {
			if version == alpHistogramPredictiveVersion {
				numeric = append(numeric, 0)
				numeric = alpEncodePatchedIntegers(numeric, integers[:used])
				end := len(numeric)
				// A constant block cannot benefit from trying another predictor.
				if end-start > 15 {
					numeric = append(numeric, 1)
					numeric = alpEncodePatchedIntegers(numeric, deltas[:used])
					if len(numeric)-end < end-start-4 {
						size := copy(numeric[start+4:], numeric[end:])
						numeric = numeric[:start+4+size]
					} else {
						numeric = numeric[:end]
					}
				}
			} else {
				numeric = alpEncodeIntegers(numeric, integers[:used])
			}
		} else {
			if version == alpHistogramPredictiveVersion {
				numeric = alpEncodeTemporalFloats(numeric, floats[:used], state, 2+positive+negative)
			} else {
				numeric = alpEncodeValuesWithState(numeric, floats[:used], state)
			}
		}
		binary.LittleEndian.PutUint32(numeric[start:], uint32(len(numeric)-start-4))

		blocks++
		if blocks == 1 {
			probeStart = len(numeric)
		} else {
			probeValues += used
		}
		used = 0
	}
	put := func(v uint64) {
		if enc == EncALPHistogram {
			integers[used] = v
		} else {
			floats[used] = math.Float64frombits(v)
		}
		used++
		if used == blockSize {
			flush()
		}
	}
	if it == nil {
		it = inner.Iterator(nil)
		first = it.Next()
	}
	integerHistogram := w.integerHistogram
	floatHistogram := w.floatHistogram
	defer func() { w.integerHistogram, w.floatHistogram = integerHistogram, floatHistogram }()
	for typ := first; typ != ValNone; typ = it.Next() {
		var sum float64
		var hint histogram.CounterResetHint
		if typ == ValHistogram {
			_, integerHistogram = it.AtHistogram(integerHistogram)
			h := integerHistogram
			if len(hints) == 0 {
				layout.Schema, layout.ZeroThreshold = h.Schema, h.ZeroThreshold
				layout.PositiveSpans, layout.NegativeSpans, layout.CustomValues = h.PositiveSpans, h.NegativeSpans, h.CustomValues
				positive, negative = len(h.PositiveBuckets), len(h.NegativeBuckets)
			}
			sum, hint = h.Sum, h.CounterResetHint
			fields := 2 + positive + negative
			w.fields = slices.Grow(w.fields[:0], fields)[:fields]
			w.fields[0], w.fields[1] = h.Count, h.ZeroCount
			k := 2
			for _, buckets := range [][]int64{h.PositiveBuckets, h.NegativeBuckets} {
				for _, v := range buckets {
					if version == alpHistogramPredictiveVersion {
						w.fields[k] = uint64(v)
					} else {
						w.fields[k] = uint64(v<<1) ^ uint64(v>>63)
					}
					k++
				}
			}
			clear(w.fields[k:])
			if len(hints) == 0 {
				w.previous = append(w.previous[:0], w.fields...)
				w.delta = slices.Grow(w.delta[:0], fields)[:fields]
				clear(w.delta)
				if version >= alpHistogramMetadataVersion {
					for remaining := w.fields; len(remaining) > 0; {
						take := min(blockSize, len(remaining))
						start := len(numeric)
						numeric = append(numeric, 0, 0, 0, 0)
						if version == alpHistogramPredictiveVersion {
							for j, v := range remaining[:take] {
								if len(w.fields)-len(remaining)+j >= 2 {
									integers[j] = v<<1 ^ uint64(int64(v)>>63)
								} else {
									integers[j] = v
								}
							}
							numeric = alpEncodePatchedIntegers(numeric, integers[:take])
						} else {
							numeric = alpEncodeIntegers(numeric, remaining[:take])
						}
						binary.LittleEndian.PutUint32(numeric[start:], uint32(len(numeric)-start-4))
						remaining = remaining[take:]
					}
				} else {
					for _, v := range w.fields {
						numeric = binary.LittleEndian.AppendUint64(numeric, v)
					}
				}
			} else {
				alpPredictIntegersNative(w.previous, w.delta, w.fields)
				remaining := w.fields
				offset := 0
				for len(remaining) > 0 {
					take := min(blockSize-used, len(remaining))
					copy(integers[used:], remaining[:take])
					if version == alpHistogramPredictiveVersion {
						for j, d := range w.delta[offset : offset+take] {
							deltas[used+j] = d<<1 ^ uint64(int64(d)>>63)
						}
					}
					offset += take
					used += take
					remaining = remaining[take:]
					if used == blockSize {
						flush()
					}
				}
			}
		} else {
			_, floatHistogram = it.AtFloatHistogram(floatHistogram)
			h := floatHistogram
			if len(hints) == 0 {
				layout.Schema, layout.ZeroThreshold = h.Schema, h.ZeroThreshold
				layout.PositiveSpans, layout.NegativeSpans, layout.CustomValues = h.PositiveSpans, h.NegativeSpans, h.CustomValues
				positive, negative = len(h.PositiveBuckets), len(h.NegativeBuckets)
			}
			sum, hint = h.Sum, h.CounterResetHint
			put(math.Float64bits(h.Count))
			put(math.Float64bits(h.ZeroCount))
			for _, buckets := range [][]float64{h.PositiveBuckets, h.NegativeBuckets} {
				for _, v := range buckets {
					put(math.Float64bits(v))
				}
			}
			if value.IsStaleNaN(sum) {
				for range positive + negative {
					put(0)
				}
			}
		}

		// Only committed blocks count toward this lower bound. No discarded scratch
		// or predicted future savings can cause a profitable candidate to exceed it.
		if budget > 0 && len(numeric)+len(w.times) > budget {
			return nil, nil
		}
		// Ignore the initial predictor transient. Require two complete later blocks
		// and a generous margin before extrapolating the prefix to the full stream.
		if budget > 0 && len(hints) == 31 && n >= 96 && blocks >= 3 {
			projected := int64(len(numeric)-probeStart) * int64(n) * int64(2+positive+negative) / int64(probeValues)
			if projected > int64(budget)*5/4 {
				return nil, nil
			}
		}
		hints = append(hints, byte(hint))
		w.samples[sumUsed] = alpSample{st: it.AtST(), t: it.AtT(), v: sum}
		sumUsed++
		if sumUsed == alpBlockSize {
			w.times = alpEncodeSamplesWithState(w.times, w.samples[:sumUsed], &w.sums)
			sumUsed = 0
		}
	}
	if it.Err() != nil {
		return nil, it.Err()
	}
	if len(hints) != n {
		return nil, errInvalidALP
	}
	flush()
	if sumUsed != 0 {
		w.times = alpEncodeSamplesWithState(w.times, w.samples[:sumUsed], &w.sums)
	}

	// The streams are complete, so reserve their final owned output once.
	var sizeScratch [binary.MaxVarintLen64]byte
	metadataSize := 4 + binary.PutVarint(sizeScratch[:], int64(layout.Schema)) + 8
	for _, spans := range [][]histogram.Span{layout.PositiveSpans, layout.NegativeSpans} {
		metadataSize += binary.PutUvarint(sizeScratch[:], uint64(len(spans)))
		for _, span := range spans {
			metadataSize += binary.PutVarint(sizeScratch[:], int64(span.Offset)) + binary.PutUvarint(sizeScratch[:], uint64(span.Length))
		}
	}
	metadataSize += binary.PutUvarint(sizeScratch[:], uint64(len(layout.CustomValues))) + 8*len(layout.CustomValues) + 4
	if version >= alpHistogramMetadataVersion {
		metadataSize += (n + 3) / 4
	} else {
		metadataSize += n
	}
	totalSize := metadataSize + len(w.times) + len(numeric)
	if budget > 0 && totalSize > budget {
		return nil, nil
	}
	dst := binary.BigEndian.AppendUint16(make([]byte, 0, totalSize), uint16(n))
	dst = append(dst, version, header)
	dst = binary.AppendVarint(dst, int64(layout.Schema))
	dst = binary.LittleEndian.AppendUint64(dst, math.Float64bits(layout.ZeroThreshold))
	dst = alpAppendSpans(dst, layout.PositiveSpans)
	dst = alpAppendSpans(dst, layout.NegativeSpans)
	dst = binary.AppendUvarint(dst, uint64(len(layout.CustomValues)))
	for _, v := range layout.CustomValues {
		dst = binary.LittleEndian.AppendUint64(dst, math.Float64bits(v))
	}
	if version >= alpHistogramMetadataVersion {
		for i := 0; i < len(hints); i += 4 {
			var packed byte
			for j := 0; j < min(4, len(hints)-i); j++ {
				packed |= hints[i+j] << uint(2*j)
			}
			dst = append(dst, packed)
		}
	} else {
		dst = append(dst, hints...)
	}
	b = w.times
	dst = binary.LittleEndian.AppendUint32(dst, uint32(len(b)))
	dst = append(dst, b...)
	return append(dst, numeric...), nil
}

// Iterator captures an immutable serialization; reuse retains only owned buffers.
func (c *ALPHistogramChunk) Iterator(reuse Iterator) Iterator {
	if c.inner != nil && c.encoded == nil {
		// Capture mutable legacy bytes without running a parameter search under the
		// series lock. The copy also isolates the final partially written byte.
		it, ok := reuse.(*alpHistogramSnapshotIterator)
		if !ok {
			it = &alpHistogramSnapshotIterator{}
		}
		it.data = append(it.data[:0], c.inner.Bytes()...)
		if it.chunk == nil || it.chunk.Encoding() != c.inner.Encoding() {
			it.chunk, _ = NewEmptyChunk(c.inner.Encoding())
		}
		it.chunk.Reset(it.data)
		it.Iterator = it.chunk.Iterator(it.Iterator)
		return it
	}
	it, ok := reuse.(*alpHistogramIterator)
	if !ok {
		it = &alpHistogramIterator{}
	}
	it.reset(c.Bytes(), c.encoding)
	if c.err != nil {
		it.err = c.err
	}
	return it
}

// alpHistogramSnapshotIterator owns its mutable-source snapshot and retains no
// references to the writer. Its buffers may be reused only by its next reset.
type alpHistogramSnapshotIterator struct {
	Iterator
	data  []byte
	chunk Chunk
}

type alpHistogramIterator struct {
	times                        Iterator
	numbers                      alpHistogramNumbers
	layout                       histogram.FloatHistogram
	h                            histogram.Histogram
	fh                           histogram.FloatHistogram
	hints                        []byte
	packedHints                  bool
	n, index, positive, negative int
	enc                          Encoding
	err                          error
}

func (it *alpHistogramIterator) reset(src []byte, enc Encoding) {
	it.enc, it.err, it.index = enc, nil, -1
	it.n, it.positive, it.negative = 0, 0, 0
	it.hints = nil
	it.numbers.src, it.numbers.err = nil, nil
	it.numbers.remaining, it.numbers.index, it.numbers.decoded = 0, 0, 0
	it.numbers.integer = enc == EncALPHistogram
	it.numbers.first = it.numbers.integer
	if len(src) < 4 || !alpValidHistogramVersion(src[2], enc) || src[3]&^CounterResetHeaderMask != 0 {
		it.err = errInvalidALP
		return
	}
	it.packedHints = src[2] >= alpHistogramMetadataVersion
	it.numbers.predictive = src[2] == alpHistogramPredictiveVersion
	it.numbers.predictor = 0
	it.numbers.compactFirst = it.packedHints
	it.numbers.blockSize = alpMaxBlockSize
	if enc == EncALPHistogram && src[2] >= alpHistogramCompactVersion {
		it.numbers.blockSize = alpBlockSize
	}
	it.n = int(binary.BigEndian.Uint16(src))
	if it.n > MaxSamplesPerALPHistogramChunk {
		it.err = errInvalidALP
		return
	}
	src = src[4:]
	if it.n == 0 {
		if len(src) != 0 {
			it.err = errInvalidALP
		}
		return
	}
	schema, k := binary.Varint(src)
	if k <= 0 || schema < math.MinInt32 || schema > math.MaxInt32 || len(src)-k < 8 {
		it.err = errInvalidALP
		return
	}
	src = src[k:]
	it.layout.Schema = int32(schema)
	it.layout.ZeroThreshold = math.Float64frombits(binary.LittleEndian.Uint64(src))
	src = src[8:]
	readSpans := func(dst []histogram.Span) ([]histogram.Span, int) {
		count, k := binary.Uvarint(src)
		if k <= 0 || count > uint64(len(src)/2) {
			it.err = errInvalidALP
			return nil, 0
		}
		src = src[k:]
		dst = slices.Grow(dst[:0], int(count))[:int(count)]
		total := uint64(0)
		for i := range dst {
			offset, k := binary.Varint(src)
			if k <= 0 || offset < math.MinInt32 || offset > math.MaxInt32 {
				it.err = errInvalidALP
				return nil, 0
			}
			src = src[k:]
			length, k := binary.Uvarint(src)
			if k <= 0 || length > math.MaxUint32 {
				it.err = errInvalidALP
				return nil, 0
			}
			src = src[k:]
			total += length
			if total > uint64(len(src))*alpMaxBlockSize {
				it.err = errInvalidALP
				return nil, 0
			}
			dst[i] = histogram.Span{Offset: int32(offset), Length: uint32(length)}
		}
		return dst, int(total)
	}
	it.layout.PositiveSpans, it.positive = readSpans(it.layout.PositiveSpans)
	if it.err != nil {
		return
	}
	it.layout.NegativeSpans, it.negative = readSpans(it.layout.NegativeSpans)
	if it.err != nil {
		return
	}
	count, k := binary.Uvarint(src)
	if k <= 0 || count > uint64((len(src)-k)/8) {
		it.err = errInvalidALP
		return
	}
	src = src[k:]
	// Custom bounds are immutable and may have escaped through an earlier At call.
	it.layout.CustomValues = make([]float64, int(count))
	for i := range it.layout.CustomValues {
		it.layout.CustomValues[i] = math.Float64frombits(binary.LittleEndian.Uint64(src))
		src = src[8:]
	}
	hintBytes := it.n
	if it.packedHints {
		hintBytes = (it.n + 3) / 4
	}
	if len(src) < hintBytes+4 {
		it.err = errInvalidALP
		return
	}
	it.hints = src[:hintBytes]
	src = src[hintBytes:]
	if it.packedHints {
		if it.n%4 != 0 && it.hints[hintBytes-1]>>uint(2*(it.n%4)) != 0 {
			it.err = errInvalidALP
			return
		}
	} else {
		for _, hint := range it.hints {
			if hint > byte(histogram.GaugeType) {
				it.err = errInvalidALP
				return
			}
		}
	}
	size := uint64(binary.LittleEndian.Uint32(src))
	src = src[4:]
	if size > uint64(len(src)) {
		it.err = errInvalidALP
		return
	}
	var c ALPChunk
	c.Reset(src[:int(size)])
	if c.err != nil || c.NumSamples() != it.n {
		it.err = errInvalidALP
		return
	}
	it.times = c.Iterator(it.times)
	src = src[int(size):]
	fields := uint64(2) + uint64(it.positive) + uint64(it.negative)
	it.numbers.fields = int(fields)
	if fields*uint64(it.n) > uint64(len(src))*alpMaxBlockSize || fields*uint64(it.n) > uint64(math.MaxInt) {
		it.err = errInvalidALP
		return
	}
	it.numbers.src, it.numbers.remaining = src, int(fields)*it.n
	if it.numbers.integer {
		if !it.numbers.compactFirst && fields > uint64(len(src)/8) {
			it.err = errInvalidALP
			return
		}
		// Check compact first-sample framing before allocating predictor arrays.
		// A forged wide layout must not turn a short malformed input into large
		// allocations merely because later decoding would reject it.
		if it.numbers.compactFirst {
			remaining := int(fields)
			data := src
			for remaining > 0 {
				if len(data) < 4 {
					it.err = errInvalidALP
					return
				}
				size := uint64(binary.LittleEndian.Uint32(data))
				if size > uint64(len(data)-4) {
					it.err = errInvalidALP
					return
				}
				block := data[4 : 4+int(size)]
				take := min(it.numbers.blockSize, remaining)
				valid := alpValidIntegerBlock(block, take, it.numbers.predictive)
				if !valid {
					it.err = errInvalidALP
					return
				}
				remaining -= take
				data = data[4+int(size):]
			}
		}
		it.numbers.previous = slices.Grow(it.numbers.previous[:0], int(fields))[:int(fields)]
		it.numbers.delta = slices.Grow(it.numbers.delta[:0], int(fields))[:int(fields)]
		clear(it.numbers.delta)
		it.numbers.remaining -= int(fields)
	}
}

func (it *alpHistogramIterator) Next() ValueType {
	if it.err != nil {
		return ValNone
	}
	if it.index+1 >= it.n {
		if it.n != 0 && (it.numbers.remaining != 0 || it.numbers.index != it.numbers.decoded || len(it.numbers.src) != 0 || it.times.Next() != ValNone || it.times.Err() != nil) {
			it.err = errInvalidALP
		}
		return ValNone
	}
	if it.times.Next() != ValFloat {
		it.err = errInvalidALP
		return ValNone
	}
	it.index++
	_, sum := it.times.At()
	var hint histogram.CounterResetHint
	if it.packedHints {
		hint = histogram.CounterResetHint((it.hints[it.index/4] >> uint(2*(it.index%4))) & 3)
	} else {
		hint = histogram.CounterResetHint(it.hints[it.index])
	}
	if it.enc == EncALPHistogram {
		h := &it.h
		h.Schema, h.ZeroThreshold, h.Sum, h.CounterResetHint = it.layout.Schema, it.layout.ZeroThreshold, sum, hint
		h.PositiveSpans, h.NegativeSpans, h.CustomValues = it.layout.PositiveSpans, it.layout.NegativeSpans, it.layout.CustomValues
		counts := it.numbers.integerSample()
		h.Count, h.ZeroCount = counts[0], counts[1]
	} else {
		h := &it.fh
		h.Schema, h.ZeroThreshold, h.Sum, h.CounterResetHint = it.layout.Schema, it.layout.ZeroThreshold, sum, hint
		h.PositiveSpans, h.NegativeSpans, h.CustomValues = it.layout.PositiveSpans, it.layout.NegativeSpans, it.layout.CustomValues
		counts := it.numbers.floatSample(2 + it.positive + it.negative)
		h.Count, h.ZeroCount = counts[0], counts[1]
		h.PositiveBuckets = counts[2 : 2+it.positive]
		h.NegativeBuckets = counts[2+it.positive:]
	}
	if it.numbers.err != nil {
		it.err = it.numbers.err
		return ValNone
	}
	if it.enc == EncALPHistogram {
		return ValHistogram
	}
	return ValFloatHistogram
}

func (it *alpHistogramIterator) Seek(t int64) ValueType {
	if it.err != nil {
		return ValNone
	}
	if it.index >= 0 && it.AtT() >= t {
		if it.enc == EncALPHistogram {
			return ValHistogram
		}
		return ValFloatHistogram
	}
	for v := it.Next(); v != ValNone; v = it.Next() {
		if it.AtT() >= t {
			return v
		}
	}
	return ValNone
}

func (*alpHistogramIterator) At() (int64, float64) { panic("cannot call At on histogram iterator") }
func (it *alpHistogramIterator) AtT() int64        { return it.times.AtT() }
func (it *alpHistogramIterator) AtST() int64       { return it.times.AtST() }
func (it *alpHistogramIterator) Err() error        { return it.err }
func (it *alpHistogramIterator) AtHistogram(h *histogram.Histogram) (int64, *histogram.Histogram) {
	if it.enc != EncALPHistogram {
		panic("cannot get integer histogram from float histogram")
	}
	if value.IsStaleNaN(it.h.Sum) {
		if h == nil {
			h = &histogram.Histogram{}
		}
		*h = histogram.Histogram{Sum: it.h.Sum}
		return it.AtT(), h
	}
	if h == nil {
		h = &histogram.Histogram{}
	}
	it.h.CopyTo(h)
	h.PositiveBuckets = slices.Grow(h.PositiveBuckets[:0], it.positive)[:it.positive]
	h.NegativeBuckets = slices.Grow(h.NegativeBuckets[:0], it.negative)[:it.negative]
	counts := it.numbers.previous[2:]
	for _, buckets := range [][]int64{h.PositiveBuckets, h.NegativeBuckets} {
		for i := range buckets {
			v := counts[i]
			if it.numbers.predictive {
				buckets[i] = int64(v)
			} else {
				buckets[i] = int64(v>>1) ^ -int64(v&1)
			}
		}
		counts = counts[len(buckets):]
	}
	return it.AtT(), h
}

func (it *alpHistogramIterator) AtFloatHistogram(h *histogram.FloatHistogram) (int64, *histogram.FloatHistogram) {
	if it.enc == EncALPHistogram {
		if value.IsStaleNaN(it.h.Sum) {
			if h == nil {
				h = &histogram.FloatHistogram{}
			}
			*h = histogram.FloatHistogram{Sum: it.h.Sum}
			return it.AtT(), h
		}
		h = it.h.ToFloat(h)
		h.PositiveBuckets = slices.Grow(h.PositiveBuckets[:0], it.positive)[:it.positive]
		h.NegativeBuckets = slices.Grow(h.NegativeBuckets[:0], it.negative)[:it.negative]
		counts := it.numbers.previous[2:]
		for _, buckets := range [][]float64{h.PositiveBuckets, h.NegativeBuckets} {
			var total float64
			for i := range buckets {
				v := counts[i]
				// Match Histogram.ToFloat's per-delta conversion and summation.
				if it.numbers.predictive {
					total += float64(int64(v))
				} else {
					total += float64(int64(v>>1) ^ -int64(v&1))
				}
				buckets[i] = total
			}
			counts = counts[len(buckets):]
		}
		return it.AtT(), h
	}
	if value.IsStaleNaN(it.fh.Sum) {
		if h == nil {
			h = &histogram.FloatHistogram{}
		}
		*h = histogram.FloatHistogram{Sum: it.fh.Sum}
		return it.AtT(), h
	}
	if h == nil {
		h = &histogram.FloatHistogram{}
	}
	it.fh.CopyTo(h)
	return it.AtT(), h
}
