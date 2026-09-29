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
	"slices"

	"github.com/prometheus/prometheus/model/histogram"
)

const (
	alpVersion         = 1
	alpHeaderSize      = 3
	alpBlockHeaderSize = 12
	alpRegularTime     = 1
	alpHasST           = 2
	alpConstantST      = 4
	// A block flush can publish more than the next sample's raw size.
	alpMaxChunkBytes = 16 * 1024
)

type alpSample struct {
	st, t int64
	v     float64
}

// ALPChunk stores lossless floating-point samples in independently decoded ALP
// blocks, with separate timestamp and optional start-timestamp streams.
// Completed blocks are immutable. Iterator creation snapshots the mutable tail,
// allowing subsequent appends while existing iterators run without a lock.
type ALPChunk struct {
	sealed     []byte
	pending    []alpSample
	serialized []byte
	numSamples int
	err        error
	validated  bool
	state      alpEncodeState
}

// NewALPChunk returns an empty ALP chunk.
func NewALPChunk() *ALPChunk { return &ALPChunk{validated: true} }

// Encoding returns EncALP.
func (*ALPChunk) Encoding() Encoding { return EncALP }

// NumSamples returns the number of samples without serializing pending data.
func (c *ALPChunk) NumSamples() int { return c.numSamples }

// Reset borrows stream until the next Reset. The caller must not mutate it.
// Existing iterators retain their snapshots. Reset(nil) releases all buffers.
func (c *ALPChunk) Reset(stream []byte) {
	*c = ALPChunk{}
	if len(stream) == 0 {
		c.validated = true
		return
	}
	c.serialized = slices.Clip(stream)
	if len(stream) < alpHeaderSize || stream[2] != alpVersion {
		c.err = errInvalidALP
		return
	}
	c.numSamples = int(binary.BigEndian.Uint16(stream))
	c.sealed = slices.Clip(stream[alpHeaderSize:])
}

// Bytes returns a complete serialized snapshot, including a partial last block.
// The caller must not modify it. Later appends never mutate a returned snapshot.
// Serialization is cached until the next append; size checks must use
// IsFloatChunkFull to avoid repeatedly compressing the mutable tail.
func (c *ALPChunk) Bytes() []byte {
	if c.serialized != nil {
		return c.serialized
	}
	b := make([]byte, alpHeaderSize, alpHeaderSize+len(c.sealed)+len(c.pending)*8)
	binary.BigEndian.PutUint16(b, uint16(c.numSamples))
	b[2] = alpVersion
	b = append(b, c.sealed...)
	if len(c.pending) > 0 {
		b = alpEncodeSamplesWithState(b, c.pending, &c.state)
	}
	c.serialized = b
	return b
}

// Compact materializes the pending block and releases the mutable tail. Future
// appends remain supported and will start a new independent block.
func (c *ALPChunk) Compact() {
	if c.err != nil {
		return
	}
	validated := c.validated
	b := c.Bytes()
	c.Reset(b)
	c.validated = validated
}

// Appender returns an appender, validating a chunk reconstructed from bytes
// before allowing mutation. Appenders must not be used concurrently.
func (c *ALPChunk) Appender() (Appender, error) {
	if c.err != nil {
		return nil, c.err
	}
	if !c.validated {
		it := c.Iterator(nil)
		for it.Next() != ValNone {
		}
		if err := it.Err(); err != nil {
			return nil, err
		}
		c.validated = true
	}
	return &alpAppender{c: c}, nil
}

type alpAppender struct{ c *ALPChunk }

func (a *alpAppender) Append(st, t int64, v float64) {
	c := a.c
	if c.numSamples == math.MaxUint16 {
		panic("chunk capacity exceeded")
	}
	c.pending = append(c.pending, alpSample{st: st, t: t, v: v})
	c.numSamples++
	c.serialized = nil
	if len(c.pending) == alpBlockSize {
		c.sealed = alpEncodeSamplesWithState(c.sealed, c.pending, &c.state)
		c.pending = c.pending[:0]
	}
}

func (*alpAppender) AppendHistogram(Appender, int64, int64, *histogram.Histogram, bool) (Chunk, bool, Appender, error) {
	panic("cannot append a histogram to an ALP chunk")
}

func (*alpAppender) AppendFloatHistogram(Appender, int64, int64, *histogram.FloatHistogram, bool) (Chunk, bool, Appender, error) {
	panic("cannot append a float histogram to an ALP chunk")
}

// Iterator captures a snapshot and optionally reuses an ALP iterator's buffers.
// Iterator creation must be serialized with mutation; subsequent iteration can
// run concurrently with appending. Each iterator owns its decoded buffers.
func (c *ALPChunk) Iterator(reuse Iterator) Iterator {
	it, ok := reuse.(*alpIterator)
	if !ok {
		it = &alpIterator{}
	}
	it.encoded = c.sealed
	it.tail = append(it.tail[:0], c.pending...)
	it.remaining = c.numSamples
	it.index, it.decoded = -1, 0
	it.err = c.err
	it.exhausted = false
	return it
}

type alpIterator struct {
	encoded                     []byte
	tail                        []alpSample
	remaining, index, decoded   int
	timestamps, startTimestamps []int64
	values                      []float64
	scratch                     alpDecodeScratch
	err                         error
	exhausted                   bool
	constantST                  bool
	st                          int64
}

func (it *alpIterator) Next() ValueType {
	if it.exhausted || it.err != nil {
		it.exhausted = true
		return ValNone
	}
	if it.index+1 < it.decoded {
		it.index++
		return ValFloat
	}
	if it.remaining == 0 {
		if len(it.encoded) != 0 || len(it.tail) != 0 {
			it.err = errInvalidALP
		}
		it.exhausted = true
		return ValNone
	}
	if len(it.encoded) != 0 {
		it.decoded, it.encoded, it.err = alpDecodeSamples(it, it.encoded)
		if it.err != nil || it.decoded > it.remaining-len(it.tail) {
			it.err = errInvalidALP
			it.exhausted = true
			return ValNone
		}
	} else {
		if len(it.tail) == 0 || len(it.tail) != it.remaining {
			it.err = errInvalidALP
			it.exhausted = true
			return ValNone
		}
		it.decoded = len(it.tail)
		it.resize(it.decoded)
		it.constantST = false
		it.startTimestamps = slices.Grow(it.startTimestamps[:0], it.decoded)[:it.decoded]
		for i, s := range it.tail {
			it.timestamps[i], it.startTimestamps[i], it.values[i] = s.t, s.st, s.v
		}
		it.tail = it.tail[:0]
	}
	it.remaining -= it.decoded
	it.index = 0
	return ValFloat
}

func (it *alpIterator) resize(n int) {
	it.timestamps = slices.Grow(it.timestamps[:0], n)[:n]
	it.values = slices.Grow(it.values[:0], n)[:n]
}

func (it *alpIterator) Seek(t int64) ValueType {
	if it.err != nil {
		return ValNone
	}
	if it.index >= 0 && it.index < it.decoded && it.timestamps[it.index] >= t {
		return ValFloat
	}
	for it.Next() != ValNone {
		if it.timestamps[it.index] >= t {
			return ValFloat
		}
	}
	return ValNone
}

func (it *alpIterator) At() (int64, float64) { return it.timestamps[it.index], it.values[it.index] }
func (it *alpIterator) AtT() int64           { return it.timestamps[it.index] }
func (it *alpIterator) AtST() int64 {
	if it.constantST {
		return it.st
	}
	return it.startTimestamps[it.index]
}
func (it *alpIterator) Err() error { return it.err }
func (*alpIterator) AtHistogram(*histogram.Histogram) (int64, *histogram.Histogram) {
	panic("cannot call ALP iterator AtHistogram")
}

func (*alpIterator) AtFloatHistogram(*histogram.FloatHistogram) (int64, *histogram.FloatHistogram) {
	panic("cannot call ALP iterator AtFloatHistogram")
}

func alpEncodeSamplesWithState(dst []byte, samples []alpSample, state *alpEncodeState) []byte {
	start := len(dst)
	dst = append(dst, make([]byte, alpBlockHeaderSize)...)
	flags := byte(alpRegularTime)
	delta := int64(0)
	if len(samples) > 1 {
		delta = samples[1].t - samples[0].t
	}
	constantST, hasST := true, samples[0].st != 0
	for i, s := range samples {
		if i > 1 && s.t-samples[i-1].t != delta {
			flags &^= alpRegularTime
		}
		constantST = constantST && s.st == samples[0].st
		hasST = hasST || s.st != 0
	}
	dst = binary.LittleEndian.AppendUint64(dst, uint64(samples[0].t))
	if flags&alpRegularTime != 0 {
		if len(samples) > 1 {
			dst = binary.LittleEndian.AppendUint64(dst, uint64(delta))
		}
	} else {
		previousDelta := int64(0)
		for i := 1; i < len(samples); i++ {
			d := samples[i].t - samples[i-1].t
			dst = binary.AppendVarint(dst, d-previousDelta)
			previousDelta = d
		}
	}
	timeEnd := len(dst)
	if hasST {
		flags |= alpHasST
		if constantST {
			flags |= alpConstantST
			dst = binary.LittleEndian.AppendUint64(dst, uint64(samples[0].st))
		} else {
			previous := int64(0)
			for _, s := range samples {
				dst = binary.AppendVarint(dst, s.st-previous)
				previous = s.st
			}
		}
	}
	stEnd := len(dst)
	var values [alpBlockSize]float64
	for i, s := range samples {
		values[i] = s.v
	}
	dst = alpEncodeValuesWithState(dst, values[:len(samples)], state)
	header := dst[start:]
	binary.LittleEndian.PutUint16(header, uint16(len(samples)))
	header[2] = flags
	binary.LittleEndian.PutUint16(header[4:], uint16(timeEnd-start-alpBlockHeaderSize))
	binary.LittleEndian.PutUint16(header[6:], uint16(stEnd-timeEnd))
	binary.LittleEndian.PutUint32(header[8:], uint32(len(dst)-stEnd))
	return dst
}

func alpDecodeSamples(it *alpIterator, src []byte) (int, []byte, error) {
	if len(src) < alpBlockHeaderSize {
		return 0, nil, errInvalidALP
	}
	n := int(binary.LittleEndian.Uint16(src))
	flags := src[2]
	timeSize, stSize := int(binary.LittleEndian.Uint16(src[4:])), int(binary.LittleEndian.Uint16(src[6:]))
	valueSize := uint64(binary.LittleEndian.Uint32(src[8:]))
	if n == 0 || n > alpMaxBlockSize || flags&^byte(7) != 0 || src[3] != 0 || flags&alpConstantST != 0 && flags&alpHasST == 0 || uint64(alpBlockHeaderSize+timeSize+stSize)+valueSize > uint64(len(src)) {
		return 0, nil, errInvalidALP
	}
	it.resize(n)
	end := alpBlockHeaderSize + timeSize + stSize + int(valueSize)
	times := src[alpBlockHeaderSize : alpBlockHeaderSize+timeSize]
	if len(times) < 8 {
		return 0, nil, errInvalidALP
	}
	it.timestamps[0] = int64(binary.LittleEndian.Uint64(times))
	times = times[8:]
	if flags&alpRegularTime != 0 {
		if n == 1 {
			if len(times) != 0 {
				return 0, nil, errInvalidALP
			}
		} else {
			if len(times) != 8 {
				return 0, nil, errInvalidALP
			}
			delta := int64(binary.LittleEndian.Uint64(times))
			for i := 1; i < n; i++ {
				it.timestamps[i] = it.timestamps[i-1] + delta
			}
		}
	} else {
		delta := int64(0)
		for i := 1; i < n; i++ {
			dd, consumed := binary.Varint(times)
			if consumed <= 0 {
				return 0, nil, errInvalidALP
			}
			delta += dd
			it.timestamps[i] = it.timestamps[i-1] + delta
			times = times[consumed:]
		}
		if len(times) != 0 {
			return 0, nil, errInvalidALP
		}
	}
	sts := src[alpBlockHeaderSize+timeSize : alpBlockHeaderSize+timeSize+stSize]
	it.constantST = true
	it.st = 0
	switch {
	case flags&alpHasST == 0:
		if len(sts) != 0 {
			return 0, nil, errInvalidALP
		}
	case flags&alpConstantST != 0:
		if len(sts) != 8 {
			return 0, nil, errInvalidALP
		}
		it.st = int64(binary.LittleEndian.Uint64(sts))
	default:
		it.constantST = false
		it.startTimestamps = slices.Grow(it.startTimestamps[:0], n)[:n]
		previous := int64(0)
		for i := range n {
			d, consumed := binary.Varint(sts)
			if consumed <= 0 {
				return 0, nil, errInvalidALP
			}
			previous += d
			it.startTimestamps[i] = previous
			sts = sts[consumed:]
		}
		if len(sts) != 0 {
			return 0, nil, errInvalidALP
		}
	}
	if err := alpDecodeValues(it.values[:n], src[end-int(valueSize):end], &it.scratch, alpDecodeNative); err != nil {
		return 0, nil, err
	}
	return n, src[end:], nil
}

// IsFloatChunkFull reports whether another float sample requires a new chunk.
// ALP uses a conservative bound including its pending tail, without serializing
// it. Other float encodings retain the existing XOR byte budget.
func IsFloatChunkFull(c Chunk) bool {
	if c.NumSamples() >= math.MaxUint16 {
		return true
	}
	if a, ok := c.(*ALPChunk); ok {
		return alpHeaderSize+len(a.sealed)+alpBlockHeaderSize+28*(len(a.pending)+1) > alpMaxChunkBytes
	}
	return len(c.Bytes()) > MaxBytesPerXORChunkBeforeAppend
}
