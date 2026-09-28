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

package tsdb

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/value"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/tsdb/tsdbutil"
)

func TestDBALPHistograms(t *testing.T) {
	for _, floating := range []bool{false, true} {
		for _, initialALP := range []bool{false, true} {
			t.Run(fmt.Sprintf("float=%v/initialALP=%v", floating, initialALP), func(t *testing.T) {
				dir := t.TempDir()
				opts := DefaultOptions()
				opts.EnableALPHistograms = initialALP
				opts.EnableSTStorage = true
				opts.EnableHistogramSTEncoding = true
				opts.FloatChunkEncoding = chunkenc.EncXOR2
				opts.EnableMemorySnapshotOnShutdown = true
				opts.OutOfOrderTimeWindow = 10000
				db := newTestDB(t, withDir(dir), withOpts(opts))
				db.DisableCompactions()
				ctx := context.Background()
				ls := labels.FromStrings("__name__", "alp_histogram")
				matcher := labels.MustNewMatcher(labels.MatchEqual, "__name__", "alp_histogram")
				appendSample := func(i int) {
					a := db.AppenderV2(ctx)
					var err error
					if floating {
						_, err = a.Append(0, ls, 1, int64(i+100), 0, nil, tsdbutil.GenerateTestFloatHistogram(int64(i)), storage.AOptions{})
					} else {
						_, err = a.Append(0, ls, 1, int64(i+100), 0, tsdbutil.GenerateTestHistogram(int64(i)), nil, storage.AOptions{})
					}
					require.NoError(t, err)
					require.NoError(t, a.Commit())
				}
				for i := range 160 {
					if i%10 != 5 {
						appendSample(i)
					}
				}
				for i := range 160 {
					if i%10 == 5 {
						appendSample(i)
					}
				}
				check := func(deleted bool) {
					q, err := db.Querier(0, 1000)
					require.NoError(t, err)
					defer func() { require.NoError(t, q.Close()) }()
					set := q.Select(ctx, true, nil, matcher)
					require.True(t, set.Next())
					it := set.At().Iterator(nil)
					count := 0
					for typ := it.Next(); typ != chunkenc.ValNone; typ = it.Next() {
						i := it.AtT() - 100
						require.Equal(t, int64(1), it.AtST())
						if deleted {
							require.True(t, i < 40 || i >= 80)
						}
						if floating {
							require.Equal(t, chunkenc.ValFloatHistogram, typ)
							_, h := it.AtFloatHistogram(nil)
							want := tsdbutil.GenerateTestFloatHistogram(i)
							want.CounterResetHint = h.CounterResetHint
							require.Equal(t, want, h)
						} else {
							require.Equal(t, chunkenc.ValHistogram, typ)
							_, h := it.AtHistogram(nil)
							want := tsdbutil.GenerateTestHistogram(i)
							want.CounterResetHint = h.CounterResetHint
							require.Equal(t, want, h)
						}
						count++
					}
					want := 160
					if deleted {
						want = 120
					}
					require.Equal(t, want, count)
					require.NoError(t, it.Err())
					require.False(t, set.Next())
					require.NoError(t, set.Err())
				}
				check(false)
				require.NoError(t, db.Close())
				opts.EnableALPHistograms = false
				db = newTestDB(t, withDir(dir), withOpts(opts))
				db.DisableCompactions()
				check(false)
				require.NoError(t, db.ApplyConfig(&config.Config{StorageConfig: config.StorageConfig{TSDBConfig: &config.TSDBConfig{ChunkEncoding: config.ChunkEncodingConfig{Floats: "xor2", Histograms: "alp"}, OutOfOrderTimeWindow: 10000}}}))
				require.NoError(t, db.CompactHead(NewRangeHead(db.Head(), 0, 259)))
				require.NoError(t, db.CompactOOOHead(ctx))
				check(false)
				require.NotEmpty(t, db.Blocks())
				for _, block := range db.Blocks() {
					q, err := NewBlockChunkQuerier(block, block.MinTime(), block.MaxTime())
					require.NoError(t, err)
					result := queryChunks(t, q, matcher)
					for _, chunks := range result {
						for _, c := range chunks {
							want := chunkenc.EncALPHistogram
							if floating {
								want = chunkenc.EncALPFloatHistogram
							}
							require.Equal(t, want, c.Chunk.Encoding())
						}
					}
				}
				require.NoError(t, db.Delete(ctx, 140, 179, matcher))
				check(true)
			})
		}
	}
}

func TestDBALPHistogramLargeCompaction(t *testing.T) {
	for _, floating := range []bool{false, true} {
		t.Run(strconv.FormatBool(floating), func(t *testing.T) {
			// Legacy non-ST chunks can exceed the mutable ALP codec's count limit.
			input := make([]sample, chunkenc.MaxSamplesPerALPHistogramChunk+1)
			for i := range input {
				input[i].t = int64(i)
				if floating {
					input[i].fh = tsdbutil.GenerateTestFloatHistogram(int64(i))
				} else {
					input[i].h = tsdbutil.GenerateTestHistogram(int64(i))
				}
			}
			ir, cr, mint, maxt := createIdxChkReaders(t, []seriesSamples{{lset: map[string]string{"a": "b"}, chunks: [][]sample{input}}})
			c, err := NewLeveledCompactor(t.Context(), nil, nil, []int64{0}, nil, nil)
			require.NoError(t, err)
			meta := &BlockMeta{MinTime: mint, MaxTime: maxt + 1}
			iw := &mockIndexWriter{}
			p := DefaultBlockPopulator{ALPHistograms: func() bool { return true }}
			err = p.PopulateBlock(c.ctx, c.metrics, c.logger, c.chunkPool, c.mergeFunc,
				[]BlockReader{&mockBReader{ir: ir, cr: cr, mint: mint, maxt: maxt}}, meta, iw, nopChunkWriter{}, AllSortedPostings)
			require.NoError(t, err)
			require.Len(t, iw.seriesChunks, 1)
			require.Greater(t, len(iw.seriesChunks[0].chunks), 1)
			count := 0
			for _, chk := range iw.seriesChunks[0].chunks {
				wantEncoding := chunkenc.EncALPHistogram
				if floating {
					wantEncoding = chunkenc.EncALPFloatHistogram
				}
				require.Equal(t, wantEncoding, chk.Chunk.Encoding())
				require.LessOrEqual(t, chk.Chunk.NumSamples(), chunkenc.MaxSamplesPerALPHistogramChunk)
				require.Equal(t, int64(count), chk.MinTime)
				it := chk.Chunk.Iterator(nil)
				for it.Next() != chunkenc.ValNone {
					require.Less(t, count, len(input))
					require.Equal(t, int64(count), it.AtT())
					if floating {
						_, h := it.AtFloatHistogram(nil)
						want := input[count].fh.Copy()
						want.CounterResetHint = h.CounterResetHint
						require.Equal(t, want, h)
					} else {
						_, h := it.AtHistogram(nil)
						want := input[count].h.Copy()
						want.CounterResetHint = h.CounterResetHint
						require.Equal(t, want, h)
					}
					count++
				}
				require.NoError(t, it.Err())
				require.Equal(t, int64(count-1), chk.MaxTime)
			}
			require.Equal(t, len(input), count)
			require.Equal(t, uint64(count), meta.Stats.NumSamples)
		})
	}
}

func TestDBALPReplay(t *testing.T) {
	for _, v2 := range []bool{false, true} {
		for _, snapshot := range []bool{false, true} {
			t.Run(fmt.Sprintf("v2=%v/snapshot=%v", v2, snapshot), func(t *testing.T) {
				ctx := context.Background()
				dir := t.TempDir()
				opts := DefaultOptions()
				opts.FloatChunkEncoding = chunkenc.EncALP
				opts.EnableSTStorage = v2
				opts.EnableMemorySnapshotOnShutdown = snapshot
				opts.OutOfOrderTimeWindow = 10000
				db := newTestDB(t, withDir(dir), withOpts(opts))
				db.DisableCompactions()
				ls := labels.FromStrings("__name__", "alp_metric")
				matcher := labels.MustNewMatcher(labels.MatchEqual, "__name__", "alp_metric")
				values := make([]float64, 300)
				for i := range values {
					values[i] = float64(i) / 100
					if i%47 == 0 {
						values[i] = math.Float64frombits(value.StaleNaN)
					}
				}
				appendSample := func(i int) {
					ts := int64(100 + i*10)
					if v2 {
						a := db.AppenderV2(ctx)
						_, err := a.Append(0, ls, ts-50, ts, values[i], nil, nil, storage.AOptions{})
						require.NoError(t, err)
						require.NoError(t, a.Commit())
					} else {
						a := db.Appender(ctx)
						_, err := a.Append(0, ls, ts, values[i])
						require.NoError(t, err)
						require.NoError(t, a.Commit())
					}
				}
				for i := range values {
					if i%10 != 5 {
						appendSample(i)
					}
				}
				for i := range values {
					if i%10 == 5 {
						appendSample(i)
					}
				}
				check := func() {
					q, err := db.Querier(0, 4000)
					require.NoError(t, err)
					set := q.Select(ctx, true, nil, matcher)
					require.True(t, set.Next())
					it := set.At().Iterator(nil)
					for i, want := range values {
						require.Equal(t, chunkenc.ValFloat, it.Next(), "sample %d", i)
						ts, got := it.At()
						require.Equal(t, int64(100+i*10), ts)
						require.Equal(t, math.Float64bits(want), math.Float64bits(got))
						if v2 {
							require.Equal(t, ts-50, it.AtST())
						}
					}
					require.Equal(t, chunkenc.ValNone, it.Next())
					require.NoError(t, it.Err())
					require.False(t, set.Next())
					require.NoError(t, set.Err())
				}
				check()
				require.Equal(t, chunkenc.EncALP, db.Head().series.getByHash(ls.Hash(), ls).headChunks.chunk.Encoding())
				require.NoError(t, db.Close())
				// Reading existing ALP chunks must not depend on the write setting.
				opts.FloatChunkEncoding = chunkenc.EncXOR2
				db = newTestDB(t, withDir(dir), withOpts(opts))
				db.DisableCompactions()
				check()
			})
		}
	}
}

func TestDBALPCompactionAndDeletion(t *testing.T) {
	for _, initialEncoding := range []chunkenc.Encoding{chunkenc.EncALP, chunkenc.EncXOR, chunkenc.EncXOR2} {
		t.Run(initialEncoding.String(), func(t *testing.T) {
			opts := DefaultOptions()
			opts.FloatChunkEncoding = initialEncoding
			db := newTestDB(t, withOpts(opts))
			db.DisableCompactions()
			ctx := context.Background()
			ls := labels.FromStrings(defaultLabelName, "0")
			for i := range 400 {
				a := db.Appender(ctx)
				_, err := a.Append(0, ls, int64(i), float64(i)/100)
				require.NoError(t, err)
				require.NoError(t, a.Commit())
			}
			// Convert finalized XOR chunks as well as newly written ALP chunks.
			require.NoError(t, db.ApplyConfig(&config.Config{StorageConfig: config.StorageConfig{TSDBConfig: &config.TSDBConfig{ChunkEncoding: config.ChunkEncodingConfig{Floats: config.FloatChunkEncodingALP}}}}))
			require.NoError(t, db.CompactHead(NewRangeHead(db.Head(), 0, 399)))
			require.Len(t, db.Blocks(), 1)
			requireBlockFloatChunkEncoding(t, db.Blocks()[0], chunkenc.EncALP)
			require.Equal(t, uint64(400), db.Blocks()[0].Meta().Stats.NumFloatSamples)
			matcher := labels.MustNewMatcher(labels.MatchEqual, defaultLabelName, "0")
			require.NoError(t, db.Delete(ctx, 120, 239, matcher))
			q, err := db.Querier(0, 399)
			require.NoError(t, err)
			set := q.Select(ctx, true, nil, matcher)
			require.True(t, set.Next())
			it := set.At().Iterator(nil)
			count := 0
			for it.Next() != chunkenc.ValNone {
				ts, v := it.At()
				require.True(t, ts < 120 || ts > 239)
				require.Equal(t, float64(ts)/100, v)
				count++
			}
			require.Equal(t, 280, count)
			require.NoError(t, it.Err())
			require.NoError(t, q.Close())
		})
	}
}

func TestDBALPEncodingReload(t *testing.T) {
	for _, st := range []bool{false, true} {
		t.Run(strconv.FormatBool(st), func(t *testing.T) {
			opts := DefaultOptions()
			opts.FloatChunkEncoding = chunkenc.EncXOR2
			opts.EnableSTStorage = st
			db := newTestDB(t, withOpts(opts))
			db.DisableCompactions()
			ls := labels.FromStrings("a", "b")
			for i, enc := range []chunkenc.Encoding{chunkenc.EncXOR2, chunkenc.EncALP, chunkenc.EncXOR2, chunkenc.EncALP} {
				name := config.FloatChunkEncodingXOR2
				histogramEncoding := "default"
				if enc == chunkenc.EncALP {
					name = config.FloatChunkEncodingALP
					histogramEncoding = "alp"
				}
				require.NoError(t, db.ApplyConfig(&config.Config{StorageConfig: config.StorageConfig{TSDBConfig: &config.TSDBConfig{ChunkEncoding: config.ChunkEncodingConfig{Floats: name, Histograms: histogramEncoding}}}}))
				a := db.AppenderV2(context.Background())
				_, err := a.Append(0, ls, 1, int64(i+100), float64(i)/10, nil, nil, storage.AOptions{})
				require.NoError(t, err)
				hls, fhls := labels.FromStrings("a", "histogram"), labels.FromStrings("a", "float_histogram")
				_, err = a.Append(0, hls, 1, int64(i+100), 0, tsdbutil.GenerateTestHistogram(int64(i)), nil, storage.AOptions{})
				require.NoError(t, err)
				_, err = a.Append(0, fhls, 1, int64(i+100), 0, nil, tsdbutil.GenerateTestFloatHistogram(int64(i)), storage.AOptions{})
				require.NoError(t, err)
				require.NoError(t, a.Commit())
				require.Equal(t, enc, db.Head().series.getByHash(ls.Hash(), ls).headChunks.chunk.Encoding())
				hEnc, fhEnc := chunkenc.EncHistogram, chunkenc.EncFloatHistogram
				if enc == chunkenc.EncALP {
					hEnc, fhEnc = chunkenc.EncALPHistogram, chunkenc.EncALPFloatHistogram
				}
				require.Equal(t, hEnc, db.Head().series.getByHash(hls.Hash(), hls).headChunks.chunk.Encoding())
				require.Equal(t, fhEnc, db.Head().series.getByHash(fhls.Hash(), fhls).headChunks.chunk.Encoding())
			}
		})
	}
}
