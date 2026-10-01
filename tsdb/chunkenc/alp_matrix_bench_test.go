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
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/value"
)

type alpMatrixCase struct {
	family, pattern, timing string
	samples, buckets        int
}

func (c alpMatrixCase) name() string {
	return fmt.Sprintf("%s/%s/n=%d/buckets=%d/time=%s", c.family, c.pattern, c.samples, c.buckets, c.timing)
}

func alpMatrixCases() []alpMatrixCase {
	var cases []alpMatrixCase
	for _, n := range []int{32, 120, 1024} {
		for _, pattern := range []string{"constant", "counter", "decimal2", "decimal6", "noisy-decimal", "computed", "random-finite", "random-bits", "stale5", "outliers20"} {
			cases = append(cases, alpMatrixCase{"float", pattern, "regular", n, 0})
			cases = append(cases, alpMatrixCase{"float", pattern, "no-st", n, 0})
		}
	}
	for _, pattern := range []string{"decimal2", "computed"} {
		cases = append(cases, alpMatrixCase{"float", pattern, "no-st-jitter", 120, 0})
		for _, timing := range []string{"jitter", "changing-st"} {
			cases = append(cases, alpMatrixCase{"float", pattern, timing, 120, 0})
		}
	}
	for _, family := range []string{"integer-histogram", "float-histogram"} {
		for _, buckets := range []int{8, 128} {
			for _, pattern := range []string{"smooth", "bursty", "gauge", "resets", "layout", "stale"} {
				cases = append(cases, alpMatrixCase{family, pattern, "regular", 120, buckets})
			}
			if family == "float-histogram" {
				for _, pattern := range []string{"fractional", "noisy-fractional"} {
					cases = append(cases, alpMatrixCase{family, pattern, "regular", 120, buckets})
				}
			}
		}
		for _, n := range []int{32, 1024} {
			for _, pattern := range []string{"smooth", "bursty"} {
				cases = append(cases, alpMatrixCase{family, pattern, "regular", n, 8})
			}
		}
		cases = append(cases, alpMatrixCase{family, "smooth", "regular", 120, 1031})
	}
	return cases
}

func (c alpMatrixCase) codecs() []string {
	if c.family == "float" {
		if c.timing == "no-st" || c.timing == "no-st-jitter" {
			return []string{"XOR", "XOR2", "ALP"}
		}
		return []string{"XOR2", "ALP"}
	}
	if c.pattern == "smooth" && c.samples == 120 && c.buckets == 8 {
		return []string{"legacy", "ALPv1", "ALPv3", "ALPv4"}
	}
	return []string{"legacy", "ALPv3", "ALPv4"}
}

type alpMatrixFixture struct {
	spec            alpMatrixCase
	times, starts   []int64
	floats          []float64
	histograms      []*histogram.Histogram
	floatHistograms []*histogram.FloatHistogram
	numericBytes    int64
}

func alpMatrixData(c alpMatrixCase) alpMatrixFixture {
	f := alpMatrixFixture{spec: c, times: make([]int64, c.samples), starts: make([]int64, c.samples)}
	r := rand.New(rand.NewSource(20260929))
	for i := range c.samples {
		f.times[i] = 1750000000000 + int64(i)*15000
		f.starts[i] = 1749999900000
		if c.timing == "jitter" || c.timing == "no-st-jitter" {
			f.times[i] += int64(r.Intn(2001) - 1000)
		}
		if c.timing == "changing-st" {
			f.starts[i] += int64(i/17) * 15000
		}
		// XOR cannot store start timestamps, so three-codec cases omit them.
		if c.timing == "no-st" || c.timing == "no-st-jitter" {
			f.starts[i] = 0
		}
	}
	if c.family == "float" {
		f.floats = make([]float64, c.samples)
		for i := range f.floats {
			v := float64(100000+i) / 100
			switch c.pattern {
			case "constant":
				v = 42
			case "counter":
				v = float64(100000 + i*13)
			case "decimal6":
				v = float64(100000+i) / 1e6
			case "noisy-decimal":
				v = float64(100000+r.Intn(10000)) / 100
			case "computed":
				v = 1 + math.Sin(float64(i))/10
			case "random-finite":
				v = r.Float64()*2000 - 1000
			case "random-bits":
				v = math.Float64frombits(r.Uint64())
			case "stale5":
				if i%20 == 0 {
					v = math.Float64frombits(value.StaleNaN)
				}
			case "outliers20":
				if i%5 == 0 {
					v = math.Pi * float64(i+1)
				}
			}
			f.floats[i] = v
		}
		f.numericBytes = int64(c.samples) * 8
		return f
	}
	populations := make([]uint64, c.buckets+2)
	for i := range c.samples {
		if c.pattern == "resets" && i%30 == 0 {
			clear(populations)
		}
		fields := c.buckets
		if c.pattern == "layout" && i >= c.samples/2 {
			fields += 2
		}
		for j := range populations {
			switch c.pattern {
			case "bursty":
				if i%17 == 0 {
					populations[j] += uint64(1 + r.Intn(1000))
				} else {
					populations[j] += uint64(r.Intn(3))
				}
			case "gauge":
				populations[j] = uint64(1 + r.Intn(10000))
			default:
				populations[j] += uint64(1 + j%7)
			}
		}
		pos := (fields + 1) / 2
		h := &histogram.Histogram{
			Schema: 1, ZeroThreshold: 0.001, ZeroCount: uint64(i + 1), Sum: float64(i+1) * 18.4,
			PositiveSpans: []histogram.Span{{Length: uint32(pos)}}, NegativeSpans: []histogram.Span{{Length: uint32(fields - pos)}},
			PositiveBuckets: make([]int64, pos), NegativeBuckets: make([]int64, fields-pos),
		}
		if c.pattern == "gauge" {
			h.CounterResetHint = histogram.GaugeType
		}
		if c.pattern == "resets" {
			h.ZeroCount = uint64(i%30 + 1)
			h.Sum = float64(i%30+1) * 18.4
			if i > 0 && i%30 == 0 {
				h.CounterResetHint = histogram.CounterReset
			}
		}
		h.Count = h.ZeroCount
		k := 0
		for _, buckets := range [][]int64{h.PositiveBuckets, h.NegativeBuckets} {
			var previous int64
			for j := range buckets {
				population := int64(populations[k])
				k++
				// A newly added layout bucket was empty in earlier samples.
				if c.pattern == "layout" && (j == len(buckets)-1) {
					population = int64(i + 1)
				}
				buckets[j] = population - previous
				previous = population
				h.Count += uint64(population)
			}
		}
		fh := h.ToFloat(nil)
		if c.pattern == "fractional" || c.pattern == "noisy-fractional" {
			fh.ZeroCount = float64(i+1) / 100
			fh.Count = fh.ZeroCount
			k = 0
			for _, buckets := range [][]float64{fh.PositiveBuckets, fh.NegativeBuckets} {
				for j := range buckets {
					if c.pattern == "fractional" {
						buckets[j] = float64((i+1)*(k%7+1)) / 100
					} else {
						buckets[j] = float64(i+1)*100 + float64(k%7) + r.Float64()
					}
					fh.Count += buckets[j]
					k++
				}
			}
		}
		if c.pattern == "stale" && i%20 == 19 {
			h = &histogram.Histogram{Sum: math.Float64frombits(value.StaleNaN)}
			fh = &histogram.FloatHistogram{Sum: math.Float64frombits(value.StaleNaN)}
		}
		f.histograms = append(f.histograms, h)
		f.floatHistograms = append(f.floatHistograms, fh)
		f.numericBytes += int64(3+len(h.PositiveBuckets)+len(h.NegativeBuckets)) * 8
	}
	return f
}

// encode includes appending all logical inputs and final serialization. Reset
// chunks are retained; a layout recode replaces only the affected last chunk.
func (f alpMatrixFixture) encode(codec string) ([]Chunk, error) {
	enc := EncXOR2
	switch f.spec.family {
	case "float":
		switch codec {
		case "XOR":
			enc = EncXOR
		case "ALP":
			enc = EncALP
		}
	case "integer-histogram":
		enc = EncHistogramST
		if codec != "legacy" {
			enc = EncALPHistogram
		}
	default:
		enc = EncFloatHistogramST
		if codec != "legacy" {
			enc = EncALPFloatHistogram
		}
	}

	c, err := NewEmptyChunk(enc)
	if err != nil {
		return nil, err
	}
	switch codec {
	case "ALPv1":
		c.(*ALPHistogramChunk).version = alpVersion
	case "ALPv3":
		c.(*ALPHistogramChunk).version = alpHistogramMetadataVersion
	case "ALPv4":
		c.(*ALPHistogramChunk).version = alpHistogramPredictiveVersion
	}
	a, err := c.Appender()
	if err != nil {
		return nil, err
	}
	chunks := []Chunk{c}
	var h histogram.Histogram
	var fh histogram.FloatHistogram
	for i := range f.spec.samples {
		if f.spec.family == "float" {
			a.Append(f.starts[i], f.times[i], f.floats[i])
			continue
		}
		var next Chunk
		var recoded bool
		// Appenders may replace a histogram's slice headers during layout recoding.
		// Copy the struct so benchmark inputs remain immutable between repetitions.
		if f.spec.family == "integer-histogram" {
			h = *f.histograms[i]
			next, recoded, a, err = a.AppendHistogram(nil, f.starts[i], f.times[i], &h, false)
		} else {
			fh = *f.floatHistograms[i]
			next, recoded, a, err = a.AppendFloatHistogram(nil, f.starts[i], f.times[i], &fh, false)
		}
		if err != nil {
			return nil, err
		}
		if next != nil {
			if recoded {
				chunks[len(chunks)-1] = next
			} else {
				chunks = append(chunks, next)
			}
		}
	}
	for _, c := range chunks {
		c.Bytes()
	}
	return chunks, nil
}

func alpMatrixPersist(chunks []Chunk) ([]Chunk, int, error) {
	result := make([]Chunk, len(chunks))
	size := 0
	for i, c := range chunks {
		var err error
		data := c.Bytes()
		size += len(data)
		result[i], err = FromData(c.Encoding(), data)
		if err != nil {
			return nil, 0, err
		}
	}
	return result, size, nil
}

type alpMatrixReader struct {
	iterators []Iterator
	h         *histogram.Histogram
	fh        *histogram.FloatHistogram
}

func (r *alpMatrixReader) scan(chunks []Chunk, asFloat bool) error {
	if len(r.iterators) != len(chunks) {
		r.iterators = make([]Iterator, len(chunks))
	}
	for i, c := range chunks {
		it := c.Iterator(r.iterators[i])
		r.iterators[i] = it
		for typ := it.Next(); typ != ValNone; typ = it.Next() {
			switch {
			case typ == ValFloat:
				_, alpBenchSink = it.At()
			case typ == ValHistogram && !asFloat:
				_, r.h = it.AtHistogram(r.h)
				alpBenchSink = r.h.Sum
			default:
				_, r.fh = it.AtFloatHistogram(r.fh)
				alpBenchSink = r.fh.Sum
			}
		}
		if err := it.Err(); err != nil {
			return err
		}
	}
	return nil
}

// BenchmarkALPMatrix compares complete compression and materialized decoding.
// Fixture construction, correctness checks, and persisted-input setup are untimed.
func BenchmarkALPMatrix(b *testing.B) {
	for _, spec := range alpMatrixCases() {
		b.Run(spec.name(), func(b *testing.B) {
			fixture := alpMatrixData(spec)
			for _, codec := range spec.codecs() {
				b.Run(codec, func(b *testing.B) {
					built, err := fixture.encode(codec)
					if err != nil {
						b.Fatal(err)
					}
					chunks, size, err := alpMatrixPersist(built)
					if err != nil {
						b.Fatal(err)
					}
					operations := []string{"encode", "decode"}
					if spec.family == "integer-histogram" {
						operations = append(operations, "decode-float")
					}
					if spec.samples == 120 && (spec.timing == "regular" || spec.timing == "no-st") && (spec.pattern == "decimal2" || spec.pattern == "random-bits" || spec.pattern == "smooth" && spec.buckets == 8) {
						operations = append(operations, "decode-cold")
					}
					for _, operation := range operations {
						b.Run(operation, func(b *testing.B) {
							b.SetBytes(fixture.numericBytes)
							b.ReportAllocs()
							var reader alpMatrixReader
							if err := reader.scan(chunks, operation == "decode-float"); err != nil {
								b.Fatal(err)
							}
							cpuUser, cpuSystem, cpuOK := alpMatrixCPU()
							for b.Loop() {
								if operation == "encode" {
									encoded, err := fixture.encode(codec)
									if err != nil {
										b.Fatal(err)
									}
									total := 0
									for _, c := range encoded {
										total += len(c.Bytes())
									}
									alpBenchSink = float64(total)
								} else {
									if operation == "decode-cold" {
										reader = alpMatrixReader{}
									}
									if err := reader.scan(chunks, operation == "decode-float"); err != nil {
										b.Fatal(err)
									}
								}
							}
							alpMatrixReportCPU(b, cpuUser, cpuSystem, cpuOK, spec.samples)
							b.ReportMetric(float64(size)/float64(spec.samples), "encoded-B/sample")
							b.ReportMetric(float64(spec.samples), "samples/op")
							b.ReportMetric(float64(len(chunks)), "chunks/op")
						})
					}
				})
			}
		})
	}
}

type alpMatrixSample struct {
	t, st int64
	bits  uint64
	h     *histogram.Histogram
	fh    *histogram.FloatHistogram
}

func alpMatrixDecoded(t *testing.T, chunks []Chunk) []alpMatrixSample {
	t.Helper()
	var result []alpMatrixSample
	for _, c := range chunks {
		it := c.Iterator(nil)
		for typ := it.Next(); typ != ValNone; typ = it.Next() {
			s := alpMatrixSample{t: it.AtT(), st: it.AtST()}
			switch typ {
			case ValFloat:
				_, v := it.At()
				s.bits = math.Float64bits(v)
			case ValHistogram:
				_, h := it.AtHistogram(nil)
				s.h = h.Copy()
				s.bits = math.Float64bits(h.Sum)
				s.h.Sum = 0
			case ValFloatHistogram:
				_, h := it.AtFloatHistogram(nil)
				s.fh = h.Copy()
				s.bits = math.Float64bits(h.Sum)
				s.fh.Sum = 0
			default:
				t.Fatalf("unexpected type %v", typ)
			}
			result = append(result, s)
		}
		require.NoError(t, it.Err())
	}
	return result
}

func TestALPMatrixFixtures(t *testing.T) {
	for _, spec := range alpMatrixCases() {
		t.Run(spec.name(), func(t *testing.T) {
			fixture := alpMatrixData(spec)
			for _, h := range fixture.histograms {
				require.NoError(t, h.Validate())
			}
			for _, h := range fixture.floatHistograms {
				require.NoError(t, h.Validate())
			}
			var expected []alpMatrixSample
			for _, codec := range spec.codecs() {
				built, err := fixture.encode(codec)
				require.NoError(t, err)
				persisted, _, err := alpMatrixPersist(built)
				require.NoError(t, err)
				got := alpMatrixDecoded(t, persisted)
				require.Len(t, got, spec.samples)
				if expected == nil {
					expected = got
				} else {
					require.Equal(t, expected, got, codec)
				}
				for i, s := range got {
					require.Equal(t, fixture.times[i], s.t)
					require.Equal(t, fixture.starts[i], s.st)
					if spec.family == "float" {
						require.Equal(t, math.Float64bits(fixture.floats[i]), s.bits)
					}
				}
				// Repeated construction must not mutate fixtures or change encoded size.
				again, err := fixture.encode(codec)
				require.NoError(t, err)
				require.Len(t, again, len(built))
				for i := range again {
					require.Equal(t, built[i].Bytes(), again[i].Bytes())
				}
			}
		})
	}
}

func alpMatrixReportCPU(b *testing.B, beforeUser, beforeSystem int64, enabled bool, samples int) {
	if !enabled {
		return
	}
	user, system, ok := alpMatrixCPU()
	if !ok {
		b.Fatal("CPU accounting became unavailable")
	}
	observations := float64(b.N) * float64(samples)
	b.ReportMetric(float64(user-beforeUser)/observations, "user-cpu-ns/sample")
	b.ReportMetric(float64(system-beforeSystem)/observations, "sys-cpu-ns/sample")
	cpu := float64(user - beforeUser + system - beforeSystem)
	b.ReportMetric(cpu/observations, "cpu-ns/sample")
	b.ReportMetric(cpu/float64(b.Elapsed().Nanoseconds())*100, "cpu-%core")
}

// BenchmarkALPMatrixTranscode isolates incremental compaction conversion. Source
// chunks are already encoded, and one encoder is reused for a repeated series.
func BenchmarkALPMatrixTranscode(b *testing.B) {
	for _, spec := range []alpMatrixCase{
		{"float", "decimal2", "regular", 120, 0},
		{"float", "computed", "regular", 120, 0},
		{"float", "random-bits", "regular", 120, 0},
		{"integer-histogram", "smooth", "regular", 120, 8},
		{"integer-histogram", "bursty", "regular", 120, 128},
		{"float-histogram", "fractional", "regular", 120, 8},
		{"float-histogram", "noisy-fractional", "regular", 120, 128},
	} {
		b.Run(spec.name(), func(b *testing.B) {
			fixture := alpMatrixData(spec)
			codec := "legacy"
			if spec.family == "float" {
				codec = "XOR2"
			}
			chunks, err := fixture.encode(codec)
			if err != nil {
				b.Fatal(err)
			}
			if len(chunks) != 1 {
				b.Fatal("expected one source chunk")
			}
			source := chunks[0]
			for _, policy := range []string{"forced", "adaptive"} {
				b.Run(policy, func(b *testing.B) {
					var e ALPEncoder
					totalBytes, accepted := 0, 0
					b.ReportAllocs()
					b.SetBytes(fixture.numericBytes)
					cpuUser, cpuSystem, cpuOK := alpMatrixCPU()
					for b.Loop() {
						var c Chunk
						var err error
						if spec.family == "float" {
							if policy == "adaptive" {
								c, err = e.RecodeFloatIfSmaller(source)
							} else {
								c, err = e.Recode(source)
							}
						} else {
							if policy == "adaptive" {
								c, err = e.RecodeHistogramIfSmaller(source)
							} else {
								c, err = e.RecodeHistogramV3(source)
							}
						}
						if err != nil {
							b.Fatal(err)
						}
						if c != nil {
							accepted++
							totalBytes += len(c.Bytes())
						} else {
							totalBytes += len(source.Bytes())
						}
					}
					alpMatrixReportCPU(b, cpuUser, cpuSystem, cpuOK, spec.samples)
					b.ReportMetric(float64(spec.samples), "samples/op")
					b.ReportMetric(float64(totalBytes)/float64(b.N)/float64(spec.samples), "encoded-B/sample")
					b.ReportMetric(float64(accepted)/float64(b.N)*100, "accepted-%")
				})
			}
		})
	}
}
