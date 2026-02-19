package app

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/market-raccoon/internal/core/insights/domain"
	"github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/result"
	"github.com/market-raccoon/internal/shared/validation"
)

const vpvrTradeEventType = "marketdata.trade"

type BuildVolumeProfileConfig struct {
	MaxBucketsPerWindow  int
	MaxLevelsPerPayload  int
	MaxOpenWindowsPerKey int
}

type BuildVolumeProfileRequest struct {
	EventType  string
	Venue      string
	Instrument string
	Timeframe  string
	TickSize   float64
	Price      float64
	Size       float64
	Side       string
	TsIngest   int64
	Seq        int64
}

type BuildVolumeProfileResponse struct {
	Emitted        bool
	DropReason     string
	Snapshot       domain.VolumeProfileSnapshotV1
	Delta          domain.VolumeProfileSnapshotV1
	IdempotencyKey string
}

type BuildVolumeProfile struct {
	cfg    BuildVolumeProfileConfig
	states map[string]*vpvrPartitionState
}

type vpvrPartitionState struct {
	windows map[int64]*vpvrWindowState
	order   []int64
}

type vpvrWindowState struct {
	windowStartMs int64
	windowEndMs   int64
	buckets       map[string]*vpvrBucketState
}

type vpvrBucketState struct {
	low    float64
	high   float64
	buy    float64
	sell   float64
	total  float64
	seqMin int64
	seqMax int64
}

type vpvrNormalizedTrade struct {
	venue      string
	instrument string
	timeframe  string
	price      float64
	size       float64
	side       string
	tickSize   float64
	tsIngest   int64
	seq        int64
}

func NewBuildVolumeProfile() *BuildVolumeProfile {
	return NewBuildVolumeProfileWithConfig(BuildVolumeProfileConfig{})
}

func NewBuildVolumeProfileWithConfig(cfg BuildVolumeProfileConfig) *BuildVolumeProfile {
	if cfg.MaxBucketsPerWindow <= 0 {
		cfg.MaxBucketsPerWindow = domain.VPVRCapBucketsPerWindow
	}
	if cfg.MaxLevelsPerPayload <= 0 {
		cfg.MaxLevelsPerPayload = domain.VPVRCapLevelsPerPayload
	}
	if cfg.MaxOpenWindowsPerKey <= 0 {
		cfg.MaxOpenWindowsPerKey = domain.VPVRCapOpenWindowsPerKey
	}
	return &BuildVolumeProfile{
		cfg:    cfg,
		states: make(map[string]*vpvrPartitionState),
	}
}

// Snapshot returns the latest in-memory volume profile snapshot for a key.
func (uc *BuildVolumeProfile) Snapshot(venue, instrument, timeframe string) (domain.VolumeProfileSnapshotV1, *problem.Problem) {
	if p := validation.Collect(
		validation.NonEmptyString("venue", venue),
		validation.NonEmptyString("instrument", instrument),
		validation.NonEmptyString("timeframe", timeframe),
	); p != nil {
		return domain.VolumeProfileSnapshotV1{}, p
	}

	nreq := vpvrNormalizedTrade{
		venue:      naming.CanonicalVenue(venue),
		instrument: naming.CanonicalInstrument(instrument),
		timeframe:  strings.ToLower(strings.TrimSpace(timeframe)),
	}
	if _, ok := domain.VPVRTimeframes[nreq.timeframe]; !ok {
		return domain.VolumeProfileSnapshotV1{}, problem.New(problem.ValidationFailed, "vpvr timeframe is unsupported")
	}

	key := nreq.venue + "|" + nreq.instrument + "|" + nreq.timeframe
	ps, ok := uc.states[key]
	if !ok || len(ps.order) == 0 {
		return domain.VolumeProfileSnapshotV1{}, problem.Newf(problem.NotFound, "vpvr snapshot not found for %s/%s/%s", venue, instrument, timeframe)
	}
	windowStart := ps.order[len(ps.order)-1]
	ws, ok := ps.windows[windowStart]
	if !ok {
		return domain.VolumeProfileSnapshotV1{}, problem.Newf(problem.NotFound, "vpvr snapshot window not found for %s/%s/%s", venue, instrument, timeframe)
	}
	return uc.buildSnapshot(nreq, ws)
}

func (uc *BuildVolumeProfile) Execute(_ context.Context, req BuildVolumeProfileRequest) result.Result[BuildVolumeProfileResponse] {
	nreq, p := normalizeAndValidateVPVRRequest(req)
	if p != nil {
		return result.FailProblem[BuildVolumeProfileResponse](p)
	}

	ws, p := uc.getWindow(nreq)
	if p != nil {
		return result.FailProblem[BuildVolumeProfileResponse](p)
	}

	bucket, dropReason, p := uc.applyTrade(ws, nreq)
	if p != nil {
		return result.FailProblem[BuildVolumeProfileResponse](p)
	}
	if dropReason != "" {
		return result.Ok(BuildVolumeProfileResponse{Emitted: false, DropReason: dropReason})
	}

	snapshot, p := uc.buildSnapshot(nreq, ws)
	if p != nil {
		return result.FailProblem[BuildVolumeProfileResponse](p)
	}
	delta, p := buildDeltaFromSnapshot(snapshot, bucket)
	if p != nil {
		return result.FailProblem[BuildVolumeProfileResponse](p)
	}

	return result.Ok(BuildVolumeProfileResponse{
		Emitted:        true,
		Snapshot:       snapshot,
		Delta:          delta,
		IdempotencyKey: VolumeProfileIdempotencyKey(snapshot, bucket.low, bucket.high, bucket.seqMax),
	})
}

func VolumeProfileIdempotencyKey(snapshot domain.VolumeProfileSnapshotV1, bucketLow, bucketHigh float64, seqMax int64) string {
	return hash.HashFields(
		naming.CanonicalVenue(snapshot.Venue),
		naming.CanonicalInstrument(snapshot.Instrument),
		strings.ToLower(strings.TrimSpace(snapshot.Timeframe)),
		strconv.FormatInt(snapshot.WindowStartTs, 10),
		strconv.FormatInt(snapshot.WindowEndTs, 10),
		formatVPVRFloat(bucketLow),
		formatVPVRFloat(bucketHigh),
		strconv.Itoa(domain.VolumeProfileSnapshotVersion),
		strconv.FormatInt(seqMax, 10),
	)
}

func normalizeAndValidateVPVRRequest(req BuildVolumeProfileRequest) (vpvrNormalizedTrade, *problem.Problem) {
	if p := validation.Collect(
		validation.NonEmptyString("event_type", req.EventType),
		validation.NonEmptyString("venue", req.Venue),
		validation.NonEmptyString("instrument", req.Instrument),
		validation.NonEmptyString("timeframe", req.Timeframe),
		validation.NonEmptyString("side", req.Side),
		validation.Positive("tick_size", req.TickSize),
		validation.Positive("price", req.Price),
		validation.Positive("size", req.Size),
		validation.PositiveInt("ts_ingest", req.TsIngest),
		validation.PositiveInt("seq", req.Seq),
	); p != nil {
		return vpvrNormalizedTrade{}, p
	}

	eventType := strings.ToLower(strings.TrimSpace(req.EventType))
	if eventType != vpvrTradeEventType {
		return vpvrNormalizedTrade{}, problem.Newf(problem.ValidationFailed, "vpvr accepts only %s events, got %s", vpvrTradeEventType, eventType)
	}
	timeframe := strings.ToLower(strings.TrimSpace(req.Timeframe))
	if _, ok := domain.VPVRTimeframes[timeframe]; !ok {
		return vpvrNormalizedTrade{}, problem.New(problem.ValidationFailed, "vpvr timeframe is unsupported")
	}
	side := strings.ToLower(strings.TrimSpace(req.Side))
	if side != "buy" && side != "sell" {
		return vpvrNormalizedTrade{}, problem.New(problem.ValidationFailed, "vpvr side must be buy or sell")
	}
	return vpvrNormalizedTrade{
		venue:      naming.CanonicalVenue(req.Venue),
		instrument: naming.CanonicalInstrument(req.Instrument),
		timeframe:  timeframe,
		price:      req.Price,
		size:       req.Size,
		side:       side,
		tickSize:   req.TickSize,
		tsIngest:   req.TsIngest,
		seq:        req.Seq,
	}, nil
}

func (uc *BuildVolumeProfile) getWindow(req vpvrNormalizedTrade) (*vpvrWindowState, *problem.Problem) {
	windowMs := timeframeToVPVRWindowMs(req.timeframe)
	if windowMs <= 0 {
		return nil, problem.New(problem.ValidationFailed, "vpvr timeframe window is invalid")
	}
	windowStart := (req.tsIngest / windowMs) * windowMs
	windowEnd := windowStart + windowMs

	key := req.venue + "|" + req.instrument + "|" + req.timeframe
	ps := uc.states[key]
	if ps == nil {
		ps = &vpvrPartitionState{windows: make(map[int64]*vpvrWindowState)}
		uc.states[key] = ps
	}
	if ws, ok := ps.windows[windowStart]; ok {
		metrics.SetVPVRBuilderWindowsOpen(req.venue, req.instrument, req.timeframe, len(ps.order))
		metrics.SetVPVRBuilderBucketCount(req.venue, req.instrument, req.timeframe, len(ws.buckets))
		return ws, nil
	}
	if len(ps.order) >= uc.cfg.MaxOpenWindowsPerKey {
		oldest := ps.order[0]
		delete(ps.windows, oldest)
		ps.order = ps.order[1:]
		metrics.IncVPVRBuilderOverloadAction("window_evict")
	}
	ws := &vpvrWindowState{
		windowStartMs: windowStart,
		windowEndMs:   windowEnd,
		buckets:       make(map[string]*vpvrBucketState),
	}
	ps.windows[windowStart] = ws
	ps.order = append(ps.order, windowStart)
	metrics.SetVPVRBuilderWindowsOpen(req.venue, req.instrument, req.timeframe, len(ps.order))
	metrics.SetVPVRBuilderBucketCount(req.venue, req.instrument, req.timeframe, len(ws.buckets))
	return ws, nil
}

func (uc *BuildVolumeProfile) applyTrade(ws *vpvrWindowState, req vpvrNormalizedTrade) (*vpvrBucketState, string, *problem.Problem) {
	low, high, p := domain.AssignVPVRBucket(req.price, req.tickSize)
	if p != nil {
		return nil, "", p
	}
	bucketKey := vpvrBucketKey(low, high)
	b := ws.buckets[bucketKey]
	if b == nil {
		if len(ws.buckets) >= uc.cfg.MaxBucketsPerWindow {
			metrics.IncVPVRBuilderDrop("bucket_cap")
			return nil, "bucket_cap", nil
		}
		b = &vpvrBucketState{
			low:    low,
			high:   high,
			seqMin: req.seq,
			seqMax: req.seq,
		}
		ws.buckets[bucketKey] = b
		metrics.SetVPVRBuilderBucketCount(req.venue, req.instrument, req.timeframe, len(ws.buckets))
	} else if req.seq <= b.seqMax {
		metrics.IncVPVRBuilderReplayMismatch()
	}
	if req.side == "buy" {
		b.buy += req.size
	} else {
		b.sell += req.size
	}
	b.total = b.buy + b.sell
	if req.seq < b.seqMin {
		b.seqMin = req.seq
	}
	if req.seq > b.seqMax {
		b.seqMax = req.seq
	}
	return b, "", nil
}

func (uc *BuildVolumeProfile) buildSnapshot(req vpvrNormalizedTrade, ws *vpvrWindowState) (domain.VolumeProfileSnapshotV1, *problem.Problem) {
	buckets := make([]domain.VolumeProfileBucketV1, 0, len(ws.buckets))
	for _, b := range ws.buckets {
		buckets = append(buckets, domain.VolumeProfileBucketV1{
			PriceLow:    b.low,
			PriceHigh:   b.high,
			BuyVolume:   b.buy,
			SellVolume:  b.sell,
			TotalVolume: b.total,
			SeqMin:      b.seqMin,
			SeqMax:      b.seqMax,
		})
	}
	buckets = capVPVRLevelsByVolume(buckets, uc.cfg.MaxLevelsPerPayload)
	if len(buckets) == 0 {
		return domain.VolumeProfileSnapshotV1{}, problem.New(problem.ValidationFailed, "vpvr snapshot has no buckets")
	}
	poc := buckets[0].PriceLow
	maxTotal := buckets[0].TotalVolume
	for _, b := range buckets {
		if b.TotalVolume > maxTotal {
			maxTotal = b.TotalVolume
			poc = b.PriceLow
		}
	}
	s := domain.VolumeProfileSnapshotV1{
		Venue:         req.venue,
		Instrument:    req.instrument,
		Timeframe:     req.timeframe,
		WindowStartTs: ws.windowStartMs,
		WindowEndTs:   ws.windowEndMs,
		Buckets:       buckets,
		POCPrice:      poc,
		ValueAreaLow:  buckets[0].PriceLow,
		ValueAreaHigh: buckets[len(buckets)-1].PriceHigh,
	}
	if p := s.Validate(); p != nil {
		return domain.VolumeProfileSnapshotV1{}, p
	}
	return s, nil
}

func buildDeltaFromSnapshot(snapshot domain.VolumeProfileSnapshotV1, changed *vpvrBucketState) (domain.VolumeProfileSnapshotV1, *problem.Problem) {
	delta := domain.VolumeProfileSnapshotV1{
		Venue:         snapshot.Venue,
		Instrument:    snapshot.Instrument,
		Timeframe:     snapshot.Timeframe,
		WindowStartTs: snapshot.WindowStartTs,
		WindowEndTs:   snapshot.WindowEndTs,
		Buckets: []domain.VolumeProfileBucketV1{
			{
				PriceLow:    changed.low,
				PriceHigh:   changed.high,
				BuyVolume:   changed.buy,
				SellVolume:  changed.sell,
				TotalVolume: changed.total,
				SeqMin:      changed.seqMin,
				SeqMax:      changed.seqMax,
			},
		},
		POCPrice:      changed.low,
		ValueAreaLow:  changed.low,
		ValueAreaHigh: changed.high,
	}
	if p := delta.Validate(); p != nil {
		return domain.VolumeProfileSnapshotV1{}, p
	}
	return delta, nil
}

func timeframeToVPVRWindowMs(tf string) int64 {
	switch strings.ToLower(strings.TrimSpace(tf)) {
	case "1m":
		return int64(time.Minute / time.Millisecond)
	case "5m":
		return int64(5 * time.Minute / time.Millisecond)
	case "1h":
		return int64(time.Hour / time.Millisecond)
	case "4h":
		return int64(4 * time.Hour / time.Millisecond)
	case "1d":
		return int64(24 * time.Hour / time.Millisecond)
	default:
		return 0
	}
}

func capVPVRLevelsByVolume(in []domain.VolumeProfileBucketV1, max int) []domain.VolumeProfileBucketV1 {
	out := make([]domain.VolumeProfileBucketV1, len(in))
	copy(out, in)
	sortVPVRBucketsByPrice(out)
	if max <= 0 || len(out) <= max {
		return out
	}
	type ranked struct {
		b domain.VolumeProfileBucketV1
	}
	byVolume := make([]ranked, len(out))
	for i, b := range out {
		byVolume[i] = ranked{b: b}
	}
	slices.SortFunc(byVolume, func(a, b ranked) int {
		if a.b.TotalVolume > b.b.TotalVolume {
			return -1
		}
		if a.b.TotalVolume < b.b.TotalVolume {
			return 1
		}
		if a.b.PriceLow < b.b.PriceLow {
			return -1
		}
		if a.b.PriceLow > b.b.PriceLow {
			return 1
		}
		if a.b.PriceHigh < b.b.PriceHigh {
			return -1
		}
		if a.b.PriceHigh > b.b.PriceHigh {
			return 1
		}
		return 0
	})
	byVolume = byVolume[:max]
	capped := make([]domain.VolumeProfileBucketV1, 0, len(byVolume))
	for _, r := range byVolume {
		capped = append(capped, r.b)
	}
	sortVPVRBucketsByPrice(capped)
	return capped
}

func sortVPVRBucketsByPrice(items []domain.VolumeProfileBucketV1) {
	slices.SortFunc(items, func(a, b domain.VolumeProfileBucketV1) int {
		if a.PriceLow < b.PriceLow {
			return -1
		}
		if a.PriceLow > b.PriceLow {
			return 1
		}
		if a.PriceHigh < b.PriceHigh {
			return -1
		}
		if a.PriceHigh > b.PriceHigh {
			return 1
		}
		return 0
	})
}

func vpvrBucketKey(low, high float64) string {
	return formatVPVRFloat(low) + "|" + formatVPVRFloat(high)
}

func formatVPVRFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func MarshalVPVRSnapshotStableBytes(snapshot domain.VolumeProfileSnapshotV1) ([]byte, error) {
	return json.Marshal(snapshot)
}
