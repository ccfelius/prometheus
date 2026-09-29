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
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func alpAdaptiveSource(pattern string) Chunk {
	c := NewXOR2Chunk()
	a, _ := c.Appender()
	for i := range 120 {
		v := float64(100000+i) / 100
		switch pattern {
		case "constant":
			v = 1
		case "computed":
			v = 1 + math.Sin(float64(i))/10
		case "random":
			v = math.Float64frombits(uint64(i+1) * 0x9e3779b97f4a7c15)
		}
		a.Append(1, int64(i*15000), v)
	}
	return c
}

func TestALPAdaptiveFloat(t *testing.T) {
	for _, pattern := range []string{"decimal", "constant", "computed", "random"} {
		t.Run(pattern, func(t *testing.T) {
			source := alpAdaptiveSource(pattern)
			before := slices.Clone(source.Bytes())
			var e ALPEncoder
			got, err := e.RecodeFloatIfSmaller(source)
			require.NoError(t, err)
			require.Equal(t, before, source.Bytes())
			if pattern != "decimal" {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.LessOrEqual(t, len(got.Bytes())*100, len(before)*95)
			want, actual := source.Iterator(nil), got.Iterator(nil)
			for want.Next() != ValNone {
				require.Equal(t, ValFloat, actual.Next())
				wt, wv := want.At()
				at, av := actual.At()
				require.Equal(t, wt, at)
				require.Equal(t, want.AtST(), actual.AtST())
				require.Equal(t, math.Float64bits(wv), math.Float64bits(av))
			}
			require.Equal(t, ValNone, actual.Next())
			require.NoError(t, actual.Err())
		})
	}
	t.Run("retry changing series", func(t *testing.T) {
		var e ALPEncoder
		c, err := e.RecodeFloatIfSmaller(alpAdaptiveSource("random"))
		require.NoError(t, err)
		require.Nil(t, c)
		for range 7 {
			c, err = e.RecodeFloatIfSmaller(alpAdaptiveSource("decimal"))
			require.NoError(t, err)
			require.Nil(t, c)
		}
		c, err = e.RecodeFloatIfSmaller(alpAdaptiveSource("decimal"))
		require.NoError(t, err)
		require.NotNil(t, c)
	})
}

func BenchmarkALPAdaptiveFloat(b *testing.B) {
	for _, pattern := range []string{"decimal", "computed", "random"} {
		source := alpAdaptiveSource(pattern)
		for _, adaptive := range []bool{false, true} {
			name := "full-trial"
			if adaptive {
				name = "sampled-trial"
			}
			b.Run(pattern+"/"+name, func(b *testing.B) {
				var e ALPEncoder
				b.ReportAllocs()
				for b.Loop() {
					var c Chunk
					var err error
					if adaptive {
						c, err = e.RecodeFloatIfSmaller(source)
					} else {
						c, err = e.Recode(source)
						if err == nil && len(c.Bytes())*100 > len(source.Bytes())*95 {
							c = nil
						}
					}
					if err != nil {
						b.Fatal(err)
					}
					if c != nil {
						alpBenchSink = float64(len(c.Bytes()))
					}
				}
			})
		}
	}
}
