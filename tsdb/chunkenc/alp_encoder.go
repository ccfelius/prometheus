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

// ALPEncoder reuses small encoding hints across finalized chunks of one series.
// The zero value is ready to use. It retains no source or output buffers and is
// not safe for concurrent use. Use a new encoder at each series boundary.
type ALPEncoder struct {
	values, counts alpEncodeState
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
