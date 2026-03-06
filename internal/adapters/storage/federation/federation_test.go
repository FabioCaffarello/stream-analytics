package federation

import (
	"context"
	"testing"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

// ---------------------------------------------------------------------------
// Stubs
// ---------------------------------------------------------------------------

type stubCandleReader struct {
	candles    []aggdomain.CandleV1
	timestamps []int64
	first      *aggdomain.CandleV1
	last       *aggdomain.CandleV1
	err        *problem.Problem
}

func (s *stubCandleReader) GetCandleRange(_ context.Context, _, _, _ string, _, _ int64, _ int) ([]aggdomain.CandleV1, *problem.Problem) {
	return s.candles, s.err
}
func (s *stubCandleReader) GetCandleTimestamps(_ context.Context, _, _, _ string, _, _ int64) ([]int64, *problem.Problem) {
	return s.timestamps, s.err
}
func (s *stubCandleReader) GetFirstCandle(_ context.Context, _, _, _ string) (*aggdomain.CandleV1, *problem.Problem) {
	return s.first, s.err
}
func (s *stubCandleReader) GetLastCandle(_ context.Context, _, _, _ string) (*aggdomain.CandleV1, *problem.Problem) {
	return s.last, s.err
}

type stubStatsReader struct {
	stats      []aggdomain.StatsWindowV1
	timestamps []int64
	first      *aggdomain.StatsWindowV1
	last       *aggdomain.StatsWindowV1
	err        *problem.Problem
}

func (s *stubStatsReader) GetStatsRange(_ context.Context, _, _, _ string, _, _ int64, _ int) ([]aggdomain.StatsWindowV1, *problem.Problem) {
	return s.stats, s.err
}
func (s *stubStatsReader) GetStatsTimestamps(_ context.Context, _, _, _ string, _, _ int64) ([]int64, *problem.Problem) {
	return s.timestamps, s.err
}
func (s *stubStatsReader) GetFirstStats(_ context.Context, _, _, _ string) (*aggdomain.StatsWindowV1, *problem.Problem) {
	return s.first, s.err
}
func (s *stubStatsReader) GetLastStats(_ context.Context, _, _, _ string) (*aggdomain.StatsWindowV1, *problem.Problem) {
	return s.last, s.err
}

type stubTapeReader struct {
	tapes []aggdomain.TapeWindowV1
	err   *problem.Problem
}

func (s *stubTapeReader) GetTapeRange(_ context.Context, _, _, _ string, _, _ int64, _ int) ([]aggdomain.TapeWindowV1, *problem.Problem) {
	return s.tapes, s.err
}

type stubOIReader struct {
	ois []aggdomain.OpenInterestWindowV1
	err *problem.Problem
}

func (s *stubOIReader) GetOIRange(_ context.Context, _, _, _ string, _, _ int64, _ int) ([]aggdomain.OpenInterestWindowV1, *problem.Problem) {
	return s.ois, s.err
}

type stubDVReader struct {
	dvs []aggdomain.DeltaVolumeWindowV1
	err *problem.Problem
}

func (s *stubDVReader) GetDeltaVolumeRange(_ context.Context, _, _, _ string, _, _ int64, _ int) ([]aggdomain.DeltaVolumeWindowV1, *problem.Problem) {
	return s.dvs, s.err
}

type stubCVDReader struct {
	cvds []aggdomain.CVDWindowV1
	err  *problem.Problem
}

func (s *stubCVDReader) GetCVDRange(_ context.Context, _, _, _ string, _, _ int64, _ int) ([]aggdomain.CVDWindowV1, *problem.Problem) {
	return s.cvds, s.err
}

type stubBSReader struct {
	bss []aggdomain.BarStatsWindowV1
	err *problem.Problem
}

func (s *stubBSReader) GetBarStatsRange(_ context.Context, _, _, _ string, _, _ int64, _ int) ([]aggdomain.BarStatsWindowV1, *problem.Problem) {
	return s.bss, s.err
}

func fixedNow(ms int64) func() int64 { return func() int64 { return ms } }

// ---------------------------------------------------------------------------
// merge tests
// ---------------------------------------------------------------------------

func TestMergeByWindowStart_HotWinsOnConflict(t *testing.T) {
	hot := []aggdomain.CandleV1{
		{WindowStartTs: 100, Open: 1.0},
		{WindowStartTs: 200, Open: 2.0},
	}
	cold := []aggdomain.CandleV1{
		{WindowStartTs: 100, Open: 9.0}, // same ts — hot should win
		{WindowStartTs: 300, Open: 3.0},
	}
	merged := mergeByWindowStart(hot, cold, func(c aggdomain.CandleV1) int64 { return c.WindowStartTs })
	if len(merged) != 3 {
		t.Fatalf("got %d items, want 3", len(merged))
	}
	if merged[0].Open != 1.0 {
		t.Fatalf("ts=100: got Open=%f, want 1.0 (hot wins)", merged[0].Open)
	}
	if merged[1].WindowStartTs != 200 {
		t.Fatalf("second item: got ws=%d, want 200", merged[1].WindowStartTs)
	}
	if merged[2].WindowStartTs != 300 {
		t.Fatalf("third item: got ws=%d, want 300", merged[2].WindowStartTs)
	}
}

func TestMergeByWindowStart_EmptyInputs(t *testing.T) {
	data := []aggdomain.CandleV1{{WindowStartTs: 100}}
	ws := func(c aggdomain.CandleV1) int64 { return c.WindowStartTs }
	if len(mergeByWindowStart(nil, data, ws)) != 1 {
		t.Fatal("nil hot should return cold")
	}
	if len(mergeByWindowStart(data, nil, ws)) != 1 {
		t.Fatal("nil cold should return hot")
	}
	if len(mergeByWindowStart[aggdomain.CandleV1](nil, nil, ws)) != 0 {
		t.Fatal("both nil should return empty")
	}
}

func TestMergeTimestamps_Dedup(t *testing.T) {
	hot := []int64{100, 200, 300}
	cold := []int64{200, 300, 400}
	merged := mergeTimestamps(hot, cold)
	want := []int64{100, 200, 300, 400}
	if len(merged) != len(want) {
		t.Fatalf("got %d, want %d", len(merged), len(want))
	}
	for i, v := range want {
		if merged[i] != v {
			t.Fatalf("merged[%d]=%d, want %d", i, merged[i], v)
		}
	}
}

func TestCapSlice(t *testing.T) {
	data := []int{1, 2, 3, 4, 5}
	if len(capSlice(data, 3)) != 3 {
		t.Fatal("expected 3 elements")
	}
	if len(capSlice(data, 0)) != 5 {
		t.Fatal("limit=0 should not cap")
	}
	if len(capSlice(data, 10)) != 5 {
		t.Fatal("limit>len should not cap")
	}
}

// ---------------------------------------------------------------------------
// routing tests
// ---------------------------------------------------------------------------

func TestRoute_ColdOnly(t *testing.T) {
	// now=1000, hotWindow=500, hotBoundary=500
	// query [0, 400] → entirely before boundary → cold only
	r := route(0, 400, 500, fixedNow(1000))
	if r != routeColdOnly {
		t.Fatalf("got %d, want routeColdOnly", r)
	}
}

func TestRoute_HotOnly(t *testing.T) {
	// now=1000, hotWindow=500, hotBoundary=500
	// query [600, 900] → entirely after boundary → hot only
	r := route(600, 900, 500, fixedNow(1000))
	if r != routeHotOnly {
		t.Fatalf("got %d, want routeHotOnly", r)
	}
}

func TestRoute_Both(t *testing.T) {
	// now=1000, hotWindow=500, hotBoundary=500
	// query [400, 900] → spans boundary → both
	r := route(400, 900, 500, fixedNow(1000))
	if r != routeBoth {
		t.Fatalf("got %d, want routeBoth", r)
	}
}

func TestRoute_ExactBoundary(t *testing.T) {
	// toMs == hotBoundary → cold only (toMs <= hotBoundary)
	r := route(0, 500, 500, fixedNow(1000))
	if r != routeColdOnly {
		t.Fatalf("got %d, want routeColdOnly", r)
	}
	// fromMs == hotBoundary → hot only (fromMs >= hotBoundary)
	r = route(500, 900, 500, fixedNow(1000))
	if r != routeHotOnly {
		t.Fatalf("got %d, want routeHotOnly", r)
	}
}

// ---------------------------------------------------------------------------
// FederatedCandleReader tests
// ---------------------------------------------------------------------------

func TestFederatedCandleReader_ColdOnly_RoutesCold(t *testing.T) {
	cold := &stubCandleReader{candles: []aggdomain.CandleV1{{WindowStartTs: 100}}}
	hot := &stubCandleReader{candles: []aggdomain.CandleV1{{WindowStartTs: 999}}}
	r := NewFederatedCandleReader(hot, cold, Config{HotWindowMs: 500})
	r.nowFn = fixedNow(1000)

	// query entirely in cold zone
	res, p := r.GetCandleRange(context.Background(), "x", "x", "1m", 0, 400, 100)
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if len(res) != 1 || res[0].WindowStartTs != 100 {
		t.Fatalf("expected cold result, got %v", res)
	}
}

func TestFederatedCandleReader_HotOnly_RoutesHot(t *testing.T) {
	cold := &stubCandleReader{candles: []aggdomain.CandleV1{{WindowStartTs: 100}}}
	hot := &stubCandleReader{candles: []aggdomain.CandleV1{{WindowStartTs: 800}}}
	r := NewFederatedCandleReader(hot, cold, Config{HotWindowMs: 500})
	r.nowFn = fixedNow(1000)

	res, p := r.GetCandleRange(context.Background(), "x", "x", "1m", 600, 900, 100)
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if len(res) != 1 || res[0].WindowStartTs != 800 {
		t.Fatalf("expected hot result, got %v", res)
	}
}

func TestFederatedCandleReader_BothMerged(t *testing.T) {
	cold := &stubCandleReader{candles: []aggdomain.CandleV1{
		{WindowStartTs: 100},
		{WindowStartTs: 600}, // overlap with hot
	}}
	hot := &stubCandleReader{candles: []aggdomain.CandleV1{
		{WindowStartTs: 600, Open: 99.0}, // hot wins
		{WindowStartTs: 800},
	}}
	r := NewFederatedCandleReader(hot, cold, Config{HotWindowMs: 500})
	r.nowFn = fixedNow(1000)

	res, p := r.GetCandleRange(context.Background(), "x", "x", "1m", 0, 900, 100)
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if len(res) != 3 {
		t.Fatalf("expected 3 merged results, got %d", len(res))
	}
	// ts=600 should come from hot (Open=99)
	if res[1].Open != 99.0 {
		t.Fatalf("ts=600: expected hot Open=99, got %f", res[1].Open)
	}
}

func TestFederatedCandleReader_BothMerged_LimitApplied(t *testing.T) {
	cold := &stubCandleReader{candles: []aggdomain.CandleV1{
		{WindowStartTs: 100}, {WindowStartTs: 200}, {WindowStartTs: 300},
	}}
	hot := &stubCandleReader{candles: []aggdomain.CandleV1{
		{WindowStartTs: 600}, {WindowStartTs: 700},
	}}
	r := NewFederatedCandleReader(hot, cold, Config{HotWindowMs: 500})
	r.nowFn = fixedNow(1000)

	res, p := r.GetCandleRange(context.Background(), "x", "x", "1m", 0, 900, 3)
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if len(res) != 3 {
		t.Fatalf("expected limit=3 applied, got %d", len(res))
	}
}

func TestFederatedCandleReader_ColdNil_FallsBackToHot(t *testing.T) {
	hot := &stubCandleReader{candles: []aggdomain.CandleV1{{WindowStartTs: 100}}}
	r := NewFederatedCandleReader(hot, nil, Config{HotWindowMs: 500})
	r.nowFn = fixedNow(1000)

	res, p := r.GetCandleRange(context.Background(), "x", "x", "1m", 0, 400, 100)
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if len(res) != 1 {
		t.Fatalf("expected fallback to hot, got %d", len(res))
	}
}

func TestFederatedCandleReader_GetFirstCandle_PicksEarliest(t *testing.T) {
	cold := &stubCandleReader{first: &aggdomain.CandleV1{WindowStartTs: 50}}
	hot := &stubCandleReader{first: &aggdomain.CandleV1{WindowStartTs: 500}}
	r := NewFederatedCandleReader(hot, cold, DefaultConfig())

	c, p := r.GetFirstCandle(context.Background(), "x", "x", "1m")
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if c.WindowStartTs != 50 {
		t.Fatalf("expected earliest (50), got %d", c.WindowStartTs)
	}
}

func TestFederatedCandleReader_GetLastCandle_PicksLatest(t *testing.T) {
	cold := &stubCandleReader{last: &aggdomain.CandleV1{WindowStartTs: 500}}
	hot := &stubCandleReader{last: &aggdomain.CandleV1{WindowStartTs: 900}}
	r := NewFederatedCandleReader(hot, cold, DefaultConfig())

	c, p := r.GetLastCandle(context.Background(), "x", "x", "1m")
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if c.WindowStartTs != 900 {
		t.Fatalf("expected latest (900), got %d", c.WindowStartTs)
	}
}

func TestFederatedCandleReader_GetTimestamps_Merged(t *testing.T) {
	cold := &stubCandleReader{timestamps: []int64{100, 200}}
	hot := &stubCandleReader{timestamps: []int64{200, 300}}
	r := NewFederatedCandleReader(hot, cold, Config{HotWindowMs: 500})
	r.nowFn = fixedNow(1000)

	ts, p := r.GetCandleTimestamps(context.Background(), "x", "x", "1m", 0, 900)
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if len(ts) != 3 { // 100, 200, 300 (deduped)
		t.Fatalf("expected 3 timestamps, got %d: %v", len(ts), ts)
	}
}

// ---------------------------------------------------------------------------
// FederatedStatsReader tests
// ---------------------------------------------------------------------------

func TestFederatedStatsReader_Merge(t *testing.T) {
	cold := &stubStatsReader{stats: []aggdomain.StatsWindowV1{{WindowStartTs: 100}}}
	hot := &stubStatsReader{stats: []aggdomain.StatsWindowV1{{WindowStartTs: 600}}}
	r := NewFederatedStatsReader(hot, cold, Config{HotWindowMs: 500})
	r.nowFn = fixedNow(1000)

	res, p := r.GetStatsRange(context.Background(), "x", "x", "1m", 0, 900, 100)
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2, got %d", len(res))
	}
}

func TestFederatedStatsReader_GetFirstStats(t *testing.T) {
	cold := &stubStatsReader{first: &aggdomain.StatsWindowV1{WindowStartTs: 10}}
	hot := &stubStatsReader{first: &aggdomain.StatsWindowV1{WindowStartTs: 500}}
	r := NewFederatedStatsReader(hot, cold, DefaultConfig())

	s, p := r.GetFirstStats(context.Background(), "x", "x", "1m")
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if s.WindowStartTs != 10 {
		t.Fatalf("expected 10, got %d", s.WindowStartTs)
	}
}

// ---------------------------------------------------------------------------
// Simple federated reader tests (tape, OI, DV, CVD, bar_stats)
// ---------------------------------------------------------------------------

func TestFederatedTapeReader_Merge(t *testing.T) {
	cold := &stubTapeReader{tapes: []aggdomain.TapeWindowV1{{WindowStartTs: 100}}}
	hot := &stubTapeReader{tapes: []aggdomain.TapeWindowV1{{WindowStartTs: 600}}}
	r := NewFederatedTapeReader(hot, cold, Config{HotWindowMs: 500})
	r.nowFn = fixedNow(1000)

	res, p := r.GetTapeRange(context.Background(), "x", "x", "1s", 0, 900, 100)
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2, got %d", len(res))
	}
}

func TestFederatedTapeReader_ColdOnly(t *testing.T) {
	cold := &stubTapeReader{tapes: []aggdomain.TapeWindowV1{{WindowStartTs: 100}}}
	r := NewFederatedTapeReader(nil, cold, Config{HotWindowMs: 500})
	r.nowFn = fixedNow(1000)

	res, p := r.GetTapeRange(context.Background(), "x", "x", "1s", 0, 400, 100)
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1, got %d", len(res))
	}
}

func TestFederatedOIReader_Merge(t *testing.T) {
	cold := &stubOIReader{ois: []aggdomain.OpenInterestWindowV1{{WindowStartTs: 100}}}
	hot := &stubOIReader{ois: []aggdomain.OpenInterestWindowV1{{WindowStartTs: 600}}}
	r := NewFederatedOIReader(hot, cold, Config{HotWindowMs: 500})
	r.nowFn = fixedNow(1000)

	res, p := r.GetOIRange(context.Background(), "x", "x", "1m", 0, 900, 100)
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2, got %d", len(res))
	}
}

func TestFederatedDVReader_Merge(t *testing.T) {
	cold := &stubDVReader{dvs: []aggdomain.DeltaVolumeWindowV1{{WindowStartTs: 100}}}
	hot := &stubDVReader{dvs: []aggdomain.DeltaVolumeWindowV1{{WindowStartTs: 600}}}
	r := NewFederatedDeltaVolumeReader(hot, cold, Config{HotWindowMs: 500})
	r.nowFn = fixedNow(1000)

	res, p := r.GetDeltaVolumeRange(context.Background(), "x", "x", "1m", 0, 900, 100)
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2, got %d", len(res))
	}
}

func TestFederatedCVDReader_Merge(t *testing.T) {
	cold := &stubCVDReader{cvds: []aggdomain.CVDWindowV1{{WindowStartTs: 100}}}
	hot := &stubCVDReader{cvds: []aggdomain.CVDWindowV1{{WindowStartTs: 600}}}
	r := NewFederatedCVDReader(hot, cold, Config{HotWindowMs: 500})
	r.nowFn = fixedNow(1000)

	res, p := r.GetCVDRange(context.Background(), "x", "x", "1m", 0, 900, 100)
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2, got %d", len(res))
	}
}

func TestFederatedBSReader_Merge(t *testing.T) {
	cold := &stubBSReader{bss: []aggdomain.BarStatsWindowV1{{WindowStartTs: 100}}}
	hot := &stubBSReader{bss: []aggdomain.BarStatsWindowV1{{WindowStartTs: 600}}}
	r := NewFederatedBarStatsReader(hot, cold, Config{HotWindowMs: 500})
	r.nowFn = fixedNow(1000)

	res, p := r.GetBarStatsRange(context.Background(), "x", "x", "1m", 0, 900, 100)
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2, got %d", len(res))
	}
}

func TestFederatedBSReader_BothNil_Error(t *testing.T) {
	r := NewFederatedBarStatsReader(nil, nil, Config{HotWindowMs: 500})
	r.nowFn = fixedNow(1000)

	_, p := r.GetBarStatsRange(context.Background(), "x", "x", "1m", 0, 400, 100)
	if p == nil || p.Code != problem.Unavailable {
		t.Fatalf("expected Unavailable, got %v", p)
	}
}

func TestFederatedCVDReader_HotError_Propagated(t *testing.T) {
	hot := &stubCVDReader{err: problem.New(problem.Unavailable, "conn refused")}
	cold := &stubCVDReader{cvds: []aggdomain.CVDWindowV1{{WindowStartTs: 100}}}
	r := NewFederatedCVDReader(hot, cold, Config{HotWindowMs: 500})
	r.nowFn = fixedNow(1000)

	// hot-only route → error propagated
	_, p := r.GetCVDRange(context.Background(), "x", "x", "1m", 600, 900, 100)
	if p == nil || p.Code != problem.Unavailable {
		t.Fatalf("expected Unavailable, got %v", p)
	}
}
