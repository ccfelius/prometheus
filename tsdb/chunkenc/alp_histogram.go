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

// ALPHistogramChunk stores histogram sums and floating-point counts in ALP
// vectors. Integer counts use lossless integer frame-of-reference packing.
// The existing ST-capable histogram appender maintains mutable chunks, including
// layout recoding and counter-reset detection. Serialization builds immutable
// ALP streams, and persisted chunks decode those streams directly with SIMD.
type ALPHistogramChunk struct {
	encoding Encoding
	inner    Chunk
	encoded  []byte
	err      error
}

// NewALPHistogramChunk returns an empty integer-count histogram chunk.
func NewALPHistogramChunk() *ALPHistogramChunk {
	return &ALPHistogramChunk{encoding: EncALPHistogram, inner: NewHistogramSTChunk()}
}

// NewALPFloatHistogramChunk returns an empty floating-count histogram chunk.
func NewALPFloatHistogramChunk() *ALPHistogramChunk {
	return &ALPHistogramChunk{encoding: EncALPFloatHistogram, inner: NewFloatHistogramSTChunk()}
}

// RecodeToALPHistogram converts a finalized histogram chunk while retaining the
// counter-reset header, layout, values, and start timestamps.
func RecodeToALPHistogram(source Chunk) (Chunk, error) {
	if source.NumSamples() > MaxSamplesPerALPHistogramChunk {
		return nil, errInvalidALP
	}
	var c *ALPHistogramChunk
	switch source.Encoding() {
	case EncHistogram, EncHistogramST:
		c = NewALPHistogramChunk()
	case EncFloatHistogram, EncFloatHistogramST:
		c = NewALPFloatHistogramChunk()
	default:
		return nil, errInvalidALP
	}
	a, err := c.Appender()
	if err != nil {
		return nil, err
	}
	it := source.Iterator(nil)
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
	b := source.Bytes()
	if len(b) < histogramHeaderSize {
		return nil, errInvalidALP
	}
	flagOffset := histogramFlagPos
	if source.Encoding() == EncHistogramST || source.Encoding() == EncFloatHistogramST {
		flagOffset = 0
	}
	a.(*alpHistogramAppender).inner.(interface{ setCounterResetHeader(CounterResetHeader) }).setCounterResetHeader(CounterResetHeader(b[flagOffset] & CounterResetHeaderMask))
	c.Compact()
	return c, nil
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
	if len(b) != 0 && (len(b) < 4 || b[2] != alpVersion || b[3]&^CounterResetHeaderMask != 0 || binary.BigEndian.Uint16(b) > MaxSamplesPerALPHistogramChunk) {
		c.err = errInvalidALP
	}
}

// Bytes returns an immutable serialization, cached until the next append.
func (c *ALPHistogramChunk) Bytes() []byte {
	if c.encoded != nil {
		return c.encoded
	}
	if c.inner == nil || c.inner.NumSamples() == 0 {
		return []byte{0, 0, alpVersion, 0}
	}
	c.encoded = alpEncodeHistograms(c.inner, c.encoding)
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
		wrapped := &ALPHistogramChunk{encoding: a.c.encoding, inner: c}
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
	field                     int
	first                     bool
	src                       []byte
	floats                    [alpMaxBlockSize]float64
	ints                      [alpMaxBlockSize]uint64
	scratch                   alpDecodeScratch
	remaining, index, decoded int
	integer                   bool
	err                       error
}

func (r *alpHistogramNumbers) next() uint64 {
	if r.err != nil {
		return 0
	}
	if r.integer && r.first {
		if len(r.src) < 8 {
			r.err = errInvalidALP
			return 0
		}
		v := binary.LittleEndian.Uint64(r.src)
		r.src = r.src[8:]
		r.previous[r.field] = v
		r.field++
		if r.field == len(r.previous) {
			r.field = 0
			r.first = false
		}
		return v
	}
	if r.index == r.decoded {
		if r.remaining == 0 || len(r.src) < 4 {
			r.err = errInvalidALP
			return 0
		}
		size := uint64(binary.LittleEndian.Uint32(r.src))
		if size > uint64(len(r.src)-4) {
			r.err = errInvalidALP
			return 0
		}
		b := r.src[4 : 4+int(size)]
		r.src = r.src[4+int(size):]
		r.decoded = min(alpMaxBlockSize, r.remaining)
		r.remaining -= r.decoded
		r.index = 0
		if r.integer {
			r.err = alpDecodeIntegers(r.ints[:r.decoded], b, &r.scratch)
		} else {
			r.err = alpDecodeValues(r.floats[:r.decoded], b, &r.scratch, alpDecodeNative)
		}
		if r.err != nil {
			return 0
		}
	}
	i := r.index
	r.index++
	if r.integer {
		z := r.ints[i]
		d := uint64(int64(z>>1) ^ -int64(z&1))
		r.delta[r.field] += d
		r.previous[r.field] += r.delta[r.field]
		v := r.previous[r.field]
		r.field++
		if r.field == len(r.previous) {
			r.field = 0
		}
		return v
	}
	return math.Float64bits(r.floats[i])
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

func alpEncodeHistograms(inner Chunk, enc Encoding) []byte {
	n := inner.NumSamples()
	times := NewALPChunk()
	ta, _ := times.Appender()
	hints := make([]byte, 0, n)
	var layout histogram.FloatHistogram
	var numeric []byte
	var floats [alpMaxBlockSize]float64
	var integers [alpMaxBlockSize]uint64
	used := 0
	var previous, delta []uint64
	field := 0
	flush := func() {
		if used == 0 {
			return
		}
		start := len(numeric)
		numeric = append(numeric, 0, 0, 0, 0)
		if enc == EncALPHistogram {
			numeric = alpEncodeIntegers(numeric, integers[:used])
		} else {
			numeric = alpEncodeValues(numeric, floats[:used])
		}
		binary.LittleEndian.PutUint32(numeric[start:], uint32(len(numeric)-start-4))
		used = 0
	}
	put := func(v uint64) {
		if enc == EncALPHistogram {
			if previous == nil {
				previous = make([]uint64, 2+len(layout.PositiveBuckets)+len(layout.NegativeBuckets))
				delta = make([]uint64, len(previous))
			}
			if len(hints) == 0 {
				numeric = binary.LittleEndian.AppendUint64(numeric, v)
				previous[field] = v
				field++
				if field == len(previous) {
					field = 0
				}
				return
			}
			d := v - previous[field]
			dd := int64(d - delta[field])
			previous[field], delta[field] = v, d
			v = uint64(dd<<1) ^ uint64(dd>>63)
			field++
			if field == len(previous) {
				field = 0
			}
		}
		integers[used], floats[used] = v, math.Float64frombits(v)
		used++
		if used == alpMaxBlockSize {
			flush()
		}
	}
	it := inner.Iterator(nil)
	var integerHistogram *histogram.Histogram
	var floatHistogram *histogram.FloatHistogram
	for typ := it.Next(); typ != ValNone; typ = it.Next() {
		var sum float64
		var hint histogram.CounterResetHint
		if typ == ValHistogram {
			_, integerHistogram = it.AtHistogram(integerHistogram)
			h := integerHistogram
			if len(hints) == 0 {
				layout = *h.ToFloat(nil)
			}
			sum, hint = h.Sum, h.CounterResetHint
			put(h.Count)
			put(h.ZeroCount)
			for _, buckets := range [][]int64{h.PositiveBuckets, h.NegativeBuckets} {
				for _, v := range buckets {
					put(uint64(v<<1) ^ uint64(v>>63))
				}
			}
			if value.IsStaleNaN(sum) {
				for range len(layout.PositiveBuckets) + len(layout.NegativeBuckets) {
					put(0)
				}
			}
		} else {
			_, floatHistogram = it.AtFloatHistogram(floatHistogram)
			h := floatHistogram
			if len(hints) == 0 {
				layout = *h.Copy()
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
				for range len(layout.PositiveBuckets) + len(layout.NegativeBuckets) {
					put(0)
				}
			}
		}
		hints = append(hints, byte(hint))
		ta.Append(it.AtST(), it.AtT(), sum)
	}
	if it.Err() != nil {
		panic(it.Err())
	} // Mutable chunks contain trusted appender output.
	flush()
	dst := binary.BigEndian.AppendUint16(nil, uint16(n))
	dst = append(dst, alpVersion, byte(inner.(interface{ GetCounterResetHeader() CounterResetHeader }).GetCounterResetHeader()))
	dst = binary.AppendVarint(dst, int64(layout.Schema))
	dst = binary.LittleEndian.AppendUint64(dst, math.Float64bits(layout.ZeroThreshold))
	dst = alpAppendSpans(dst, layout.PositiveSpans)
	dst = alpAppendSpans(dst, layout.NegativeSpans)
	dst = binary.AppendUvarint(dst, uint64(len(layout.CustomValues)))
	for _, v := range layout.CustomValues {
		dst = binary.LittleEndian.AppendUint64(dst, math.Float64bits(v))
	}
	dst = append(dst, hints...)
	b := times.Bytes()
	dst = binary.LittleEndian.AppendUint32(dst, uint32(len(b)))
	dst = append(dst, b...)
	return append(dst, numeric...)
}

// Iterator captures an immutable serialization; reuse retains only owned buffers.
func (c *ALPHistogramChunk) Iterator(reuse Iterator) Iterator {
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

type alpHistogramIterator struct {
	times                        Iterator
	numbers                      alpHistogramNumbers
	layout                       histogram.FloatHistogram
	h                            histogram.Histogram
	fh                           histogram.FloatHistogram
	hints                        []byte
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
	it.numbers.first, it.numbers.field = it.numbers.integer, 0
	if len(src) < 4 || src[2] != alpVersion || src[3]&^CounterResetHeaderMask != 0 {
		it.err = errInvalidALP
		return
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
	if len(src) < it.n+4 {
		it.err = errInvalidALP
		return
	}
	it.hints = src[:it.n]
	src = src[it.n:]
	for _, hint := range it.hints {
		if hint > byte(histogram.GaugeType) {
			it.err = errInvalidALP
			return
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
	if fields*uint64(it.n) > uint64(len(src))*alpMaxBlockSize || fields*uint64(it.n) > uint64(math.MaxInt) {
		it.err = errInvalidALP
		return
	}
	it.numbers.src, it.numbers.remaining = src, int(fields)*it.n
	if it.numbers.integer {
		if fields > uint64(len(src)/8) {
			it.err = errInvalidALP
			return
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
	hint := histogram.CounterResetHint(it.hints[it.index])
	if it.enc == EncALPHistogram {
		h := &it.h
		h.Schema, h.ZeroThreshold, h.Sum, h.CounterResetHint = it.layout.Schema, it.layout.ZeroThreshold, sum, hint
		h.PositiveSpans, h.NegativeSpans, h.CustomValues = it.layout.PositiveSpans, it.layout.NegativeSpans, it.layout.CustomValues
		h.Count, h.ZeroCount = it.numbers.next(), it.numbers.next()
		h.PositiveBuckets = slices.Grow(h.PositiveBuckets[:0], it.positive)[:it.positive]
		h.NegativeBuckets = slices.Grow(h.NegativeBuckets[:0], it.negative)[:it.negative]
		for _, buckets := range [][]int64{h.PositiveBuckets, h.NegativeBuckets} {
			for i := range buckets {
				v := it.numbers.next()
				buckets[i] = int64(v>>1) ^ -int64(v&1)
			}
		}
	} else {
		h := &it.fh
		h.Schema, h.ZeroThreshold, h.Sum, h.CounterResetHint = it.layout.Schema, it.layout.ZeroThreshold, sum, hint
		h.PositiveSpans, h.NegativeSpans, h.CustomValues = it.layout.PositiveSpans, it.layout.NegativeSpans, it.layout.CustomValues
		h.Count, h.ZeroCount = math.Float64frombits(it.numbers.next()), math.Float64frombits(it.numbers.next())
		h.PositiveBuckets = slices.Grow(h.PositiveBuckets[:0], it.positive)[:it.positive]
		h.NegativeBuckets = slices.Grow(h.NegativeBuckets[:0], it.negative)[:it.negative]
		for _, buckets := range [][]float64{h.PositiveBuckets, h.NegativeBuckets} {
			for i := range buckets {
				buckets[i] = math.Float64frombits(it.numbers.next())
			}
		}
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
		return it.AtT(), &histogram.Histogram{Sum: it.h.Sum}
	}
	if h == nil {
		h = &histogram.Histogram{}
	}
	it.h.CopyTo(h)
	return it.AtT(), h
}

func (it *alpHistogramIterator) AtFloatHistogram(h *histogram.FloatHistogram) (int64, *histogram.FloatHistogram) {
	if it.enc == EncALPHistogram {
		if value.IsStaleNaN(it.h.Sum) {
			return it.AtT(), &histogram.FloatHistogram{Sum: it.h.Sum}
		}
		return it.AtT(), it.h.ToFloat(h)
	}
	if value.IsStaleNaN(it.fh.Sum) {
		return it.AtT(), &histogram.FloatHistogram{Sum: it.fh.Sum}
	}
	if h == nil {
		h = &histogram.FloatHistogram{}
	}
	it.fh.CopyTo(h)
	return it.AtT(), h
}
