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

//go:generate go run ./internal/alpgen

import "encoding/binary"

func alpMask(width int) uint64 {
	return ^uint64(0) >> (64 - width)
}

func alpLaneCount(n, lane int) int {
	return (n + alpLanes - 1 - lane) / alpLanes
}

func alpPackedSize(n, width int) int {
	short, long := n/alpLanes, n%alpLanes
	return (alpLanes-long)*((short*width+7)/8) + long*(((short+1)*width+7)/8)
}

// alpPack emits shared complete word rows followed by byte-rounded lane tails.
// The order is independent of the hardware vector width and host endianness.
func alpPack(dst []byte, values []uint64, width int) []byte {
	if width == 0 {
		return dst
	}
	var words [alpMaxBlockSize + 2*alpLanes]uint64
	for i, v := range values {
		bit := (i / alpLanes) * width
		word, shift := (bit/64)*alpLanes+i%alpLanes, uint(bit%64)
		words[word] |= v << shift
		if shift+uint(width) > 64 {
			words[word+alpLanes] |= v >> (64 - shift)
		}
	}
	common := (len(values) / alpLanes) * width / 64
	for _, word := range words[:common*alpLanes] {
		dst = binary.LittleEndian.AppendUint64(dst, word)
	}
	for lane := range alpLanes {
		remaining := (alpLaneCount(len(values), lane)*width+7)/8 - common*8
		for j := range remaining {
			word := words[(common+j/8)*alpLanes+lane]
			dst = append(dst, byte(word>>(8*(j%8))))
		}
	}
	return dst
}

// alpUnpackWords normalizes compact tails into zero-padded word rows. src must
// have the length returned by alpPackedSize. An extra zero row permits bounded
// cross-word vector loads even for the last residual and very short blocks.
func alpUnpackWords(words []uint64, src []byte, n, width int) []uint64 {
	length := (((n+alpLanes-1)/alpLanes*width+63)/64 + 1) * alpLanes
	if cap(words) < length {
		words = make([]uint64, length)
	} else {
		words = words[:length]
		// All complete rows are overwritten. Only uneven tails and the extra
		// bounded-load row can contain words not written below.
		clear(words[max(0, length-2*alpLanes):])
	}
	common := (n / alpLanes) * width / 64
	for i := range common * alpLanes {
		words[i] = binary.LittleEndian.Uint64(src[i*8:])
	}
	src = src[common*alpLanes*8:]
	for lane := range alpLanes {
		remaining := (alpLaneCount(n, lane)*width+7)/8 - common*8
		for row := common; remaining > 0; row++ {
			take := min(8, remaining)
			var word uint64
			if len(src) >= 8 {
				word = binary.LittleEndian.Uint64(src) & alpMask(take*8)
			} else {
				// Only the final lane can lack a full bounded load. Copying into
				// local padding avoids reading past an mmap or the value section.
				var tail [8]byte
				copy(tail[:], src[:take])
				word = binary.LittleEndian.Uint64(tail[:])
			}
			words[row*alpLanes+lane] = word
			src = src[take:]
			remaining -= take
		}
	}
	return words
}

func alpUnpackAt(words []uint64, i, width int) uint64 {
	bit := (i / alpLanes) * width
	word, shift := (bit/64)*alpLanes+i%alpLanes, uint(bit%64)
	v := words[word] >> shift
	if shift+uint(width) > 64 {
		v |= words[word+alpLanes] << (64 - shift)
	}
	return v & alpMask(width)
}

// alpKernel writes decoded values into caller-owned dst and retains no buffers.
// The caller validates metadata and proves the integer product fits int64.
type alpKernel func(dst []float64, words []uint64, width int, base int64, factor, exponent uint8)

func alpDecodeScalar(dst []float64, words []uint64, width int, base int64, factor, exponent uint8) {
	for i := range dst {
		q := int64(uint64(base) + alpUnpackAt(words, i, width))
		dst[i] = float64(q*alpFactors[factor]) * alpFractions[exponent]
	}
}
