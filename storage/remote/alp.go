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

package remote

import (
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/tsdb/chunks"
)

// remoteReadChunkIterator transcodes ALP into bounded XOR/XOR2 chunks. Remote
// read negotiates response types, not chunk codecs, so ALP must not escape as an
// unknown protobuf enum. Samples with start timestamps require XOR2 readers.
type remoteReadChunkIterator struct {
	converted chunks.Iterator
	source    chunks.Iterator
	values    chunkenc.Iterator
	current   chunks.Meta
	err       error
}

func (it *remoteReadChunkIterator) Next() bool {
	if it.err != nil {
		return false
	}
	for {
		if it.converted != nil {
			if it.converted.Next() {
				it.current = it.converted.At()
				return true
			}
			if it.err = it.converted.Err(); it.err != nil {
				return false
			}
			it.converted = nil
		}
		if it.values != nil {
			var samples [120]struct {
				st, t int64
				v     float64
			}
			n := 0
			hasST := false
			for n < len(samples) && it.values.Next() != chunkenc.ValNone {
				s := &samples[n]
				s.t, s.v = it.values.At()
				s.st = it.values.AtST()
				hasST = hasST || s.st != 0
				n++
			}
			if n < len(samples) {
				if it.err = it.values.Err(); it.err != nil {
					return false
				}
				it.values = nil
			}
			if n == 0 {
				continue
			}
			c, err := chunkenc.ValFloat.NewChunk(hasST, false)
			if err != nil {
				it.err = err
				return false
			}
			a, err := c.Appender()
			if err != nil {
				it.err = err
				return false
			}
			for _, s := range samples[:n] {
				a.Append(s.st, s.t, s.v)
			}
			it.current = chunks.Meta{Chunk: c, MinTime: samples[0].t, MaxTime: samples[n-1].t}
			return true
		}
		if !it.source.Next() {
			it.err = it.source.Err()
			return false
		}
		it.current = it.source.At()
		if it.current.Chunk != nil && (it.current.Chunk.Encoding() == chunkenc.EncALPHistogram || it.current.Chunk.Encoding() == chunkenc.EncALPFloatHistogram) {
			c := it.current.Chunk
			series := &storage.SeriesEntry{SampleIteratorFn: func(reuse chunkenc.Iterator) chunkenc.Iterator { return c.Iterator(reuse) }}
			it.converted = storage.NewSeriesToChunkEncoder(series).Iterator(nil)
			continue
		}
		if it.current.Chunk == nil || it.current.Chunk.Encoding() != chunkenc.EncALP {
			return true
		}
		it.values = it.current.Chunk.Iterator(nil)
	}
}

func (it *remoteReadChunkIterator) At() chunks.Meta { return it.current }
func (it *remoteReadChunkIterator) Err() error      { return it.err }
