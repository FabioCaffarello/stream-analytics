package app

import (
	"context"
	"math"
	"slices"
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

const (
	heatmapDefaultWindowMs       = int64(time.Minute / time.Millisecond)
	heatmapDefaultPriceBucketCap = 512
	heatmapDefaultSizeBucketCap  = 5
	heatmapDefaultCellsCap       = 2048
	heatmapDefaultOpenWindowsCap = 2
	heatmapDefaultMaxPayload     = 256 * 1024
)

type BuildHeatmapConfig struct {
	MaxPriceBucketsPerWindow int
	MaxSizeBuckets           int
	MaxCellsPerWindow        int
	MaxOpenWindowsPerKey     int
	MaxPayloadBytes          int
}

type BuildHeatmapRequest struct {
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

type BuildHeatmapResponse struct {
	Emitted        bool
	DropReason     string
	Artifact       domain.HeatmapArtifactV1
	IdempotencyKey string
}

type BuildHeatmap struct {
	cfg    BuildHeatmapConfig
	states map[string]*partitionState
}

type partitionState struct {
	windows map[int64]*windowState
	order   []int64
}

type windowState struct {
	windowStartMs int64
	windowEndMs   int64
	priceMult     int64
	cells         map[int64]*heatmapCellState
}

type heatmapCellState struct {
	priceMid float64
	low      float64
	high     float64
	size     string
	bid      float64
	ask      float64
	trade    float64
	seqMin   int64
	seqMax   int64
	samples  int64
}

func NewBuildHeatmap() *BuildHeatmap {
	return NewBuildHeatmapWithConfig(BuildHeatmapConfig{})
}

func NewBuildHeatmapWithConfig(cfg BuildHeatmapConfig) *BuildHeatmap {
	if cfg.MaxPriceBucketsPerWindow <= 0 {
		cfg.MaxPriceBucketsPerWindow = heatmapDefaultPriceBucketCap
	}
	if cfg.MaxSizeBuckets <= 0 {
		cfg.MaxSizeBuckets = heatmapDefaultSizeBucketCap
	}
	if cfg.MaxCellsPerWindow <= 0 {
		cfg.MaxCellsPerWindow = heatmapDefaultCellsCap
	}
	if cfg.MaxOpenWindowsPerKey <= 0 {
		cfg.MaxOpenWindowsPerKey = heatmapDefaultOpenWindowsCap
	}
	if cfg.MaxPayloadBytes <= 0 {
		cfg.MaxPayloadBytes = heatmapDefaultMaxPayload
	}
	return &BuildHeatmap{
		cfg:    cfg,
		states: make(map[string]*partitionState),
	}
}

// Snapshot returns the latest in-memory heatmap snapshot for a key.
func (uc *BuildHeatmap) Snapshot(venue, instrument, timeframe string) (domain.HeatmapArtifactV1, *problem.Problem) {
	if p := validation.Collect(
		validation.NonEmptyString("venue", venue),
		validation.NonEmptyString("instrument", instrument),
		validation.NonEmptyString("timeframe", timeframe),
	); p != nil {
		return domain.HeatmapArtifactV1{}, p
	}

	key := naming.CanonicalVenue(venue) + "|" +
		naming.CanonicalInstrument(instrument) + "|" +
		naming.NormalizeTimeframe(timeframe)
	ps, ok := uc.states[key]
	if !ok || len(ps.order) == 0 {
		return domain.HeatmapArtifactV1{}, problem.Newf(problem.NotFound, "heatmap snapshot not found for %s/%s/%s", venue, instrument, timeframe)
	}
	windowStart := ps.order[len(ps.order)-1]
	ws, ok := ps.windows[windowStart]
	if !ok {
		return domain.HeatmapArtifactV1{}, problem.Newf(problem.NotFound, "heatmap snapshot window not found for %s/%s/%s", venue, instrument, timeframe)
	}

	artifact := toArtifact(BuildHeatmapRequest{
		Venue:      venue,
		Instrument: instrument,
		Timeframe:  timeframe,
	}, ws)
	artifact = uc.trimToPayloadBudget(artifact)
	if p := artifact.Validate(); p != nil {
		return domain.HeatmapArtifactV1{}, p
	}
	return artifact, nil
}

func (uc *BuildHeatmap) Execute(_ context.Context, req BuildHeatmapRequest) result.Result[BuildHeatmapResponse] {
	if p := validateHeatmapRequest(req); p != nil {
		return result.FailProblem[BuildHeatmapResponse](p)
	}
	key := partitionKey(req)
	windowMs := timeframeToWindowMs(req.Timeframe)
	if windowMs <= 0 {
		windowMs = heatmapDefaultWindowMs
	}
	windowStart := (req.TsIngest / windowMs) * windowMs
	windowEnd := windowStart + windowMs

	ps := uc.getPartition(key)
	ws := uc.getWindow(ps, windowStart, windowEnd)
	reason := uc.apply(ws, req)
	if reason != "" {
		metrics.IncHeatmapDrop(reason)
	}

	artifact := toArtifact(req, ws)
	trimmed := uc.trimToPayloadBudget(artifact)
	if p := trimmed.Validate(); p != nil {
		return result.FailProblem[BuildHeatmapResponse](p)
	}
	metrics.SetHeatmapCells(trimmed.Venue, trimmed.Instrument, trimmed.Timeframe, len(trimmed.Cells))
	metrics.ObserveHeatmapPayloadBytes(trimmed.Venue, trimmed.Instrument, trimmed.Timeframe, estimateHeatmapPayloadSize(len(trimmed.Cells)))
	metrics.SetHeatmapQueueDepth(trimmed.Venue, trimmed.Instrument, len(ws.cells))
	metrics.ObserveHeatmapBuildLatency(trimmed.Venue, trimmed.Instrument, trimmed.Timeframe, 0)

	resp := BuildHeatmapResponse{
		Emitted:        true,
		DropReason:     reason,
		Artifact:       trimmed,
		IdempotencyKey: HeatmapArtifactIdempotencyKey(trimmed),
	}
	return result.Ok(resp)
}

func HeatmapArtifactIdempotencyKey(a domain.HeatmapArtifactV1) string {
	if len(a.Cells) == 0 {
		return ""
	}
	last := a.Cells[len(a.Cells)-1]
	return hash.NewFieldHasher().
		String(naming.CanonicalVenue(a.Venue)).
		String(naming.CanonicalInstrument(a.Instrument)).
		String(naming.NormalizeTimeframe(a.Timeframe)).
		Int64(a.WindowStartTs).
		Float64(last.PriceBucketLow).
		Float64(last.PriceBucketHigh).
		String(strings.ToUpper(strings.TrimSpace(last.SizeBucket))).
		Int64(last.SeqMax).
		Hex()
}

func (uc *BuildHeatmap) getPartition(key string) *partitionState {
	ps, ok := uc.states[key]
	if ok {
		return ps
	}
	ps = &partitionState{windows: make(map[int64]*windowState)}
	uc.states[key] = ps
	return ps
}

func (uc *BuildHeatmap) getWindow(ps *partitionState, start, end int64) *windowState {
	if ws, ok := ps.windows[start]; ok {
		return ws
	}
	if len(ps.order) >= uc.cfg.MaxOpenWindowsPerKey {
		oldest := ps.order[0]
		delete(ps.windows, oldest)
		ps.order = ps.order[1:]
	}
	ws := &windowState{
		windowStartMs: start,
		windowEndMs:   end,
		priceMult:     1,
		cells:         make(map[int64]*heatmapCellState),
	}
	ps.windows[start] = ws
	ps.order = append(ps.order, start)
	slices.Sort(ps.order)
	return ws
}

func (uc *BuildHeatmap) apply(ws *windowState, req BuildHeatmapRequest) string {
	dropReason := ""
	factor := domain.HeatmapBinFactorForTimeframe(naming.NormalizeTimeframe(req.Timeframe))
	binSize := domain.CalculateHeatmapBinSizeWithFactor(req.Price, req.TickSize, factor)
	for {
		priceIdx := bucketIndex(req.Price, binSize, ws.priceMult)
		low, high := priceBounds(priceIdx, binSize, ws.priceMult)
		sizeBucket := toSizeBucket(req.Size)
		cellKey := makeCellKey(priceIdx, sizeBucket)
		if _, exists := ws.cells[cellKey]; !exists {
			if uc.priceBucketCount(ws) >= uc.cfg.MaxPriceBucketsPerWindow {
				if uc.coarsen(ws, req.TickSize, factor) {
					dropReason = "coarsen_price_bucket"
					continue
				}
				dropReason = "price_bucket_cap"
				return dropReason
			}
		}
		c := ws.cells[cellKey]
		if c == nil {
			c = &heatmapCellState{
				priceMid: req.Price,
				low:      low,
				high:     high,
				size:     sizeBucket,
				seqMin:   req.Seq,
				seqMax:   req.Seq,
			}
			ws.cells[cellKey] = c
		}
		applyEventVolume(c, req)
		if req.Seq < c.seqMin {
			c.seqMin = req.Seq
		}
		if req.Seq > c.seqMax {
			c.seqMax = req.Seq
		}
		c.samples++
		break
	}
	if len(ws.cells) > uc.cfg.MaxCellsPerWindow {
		ws.cells = keepTopCells(ws.cells, uc.cfg.MaxCellsPerWindow)
		dropReason = "top_n_cells"
	}
	return dropReason
}

func (uc *BuildHeatmap) coarsen(ws *windowState, tickSize, binFactor float64) bool {
	if ws.priceMult >= 64 {
		return false
	}
	ws.priceMult *= 2
	next := make(map[int64]*heatmapCellState, len(ws.cells))
	for _, c := range ws.cells {
		cellBin := domain.CalculateHeatmapBinSizeWithFactor(c.priceMid, tickSize, binFactor)
		priceIdx := bucketIndex(c.priceMid, cellBin, ws.priceMult)
		low, high := priceBounds(priceIdx, cellBin, ws.priceMult)
		key := makeCellKey(priceIdx, c.size)
		dst := next[key]
		if dst == nil {
			dst = &heatmapCellState{
				priceMid: c.priceMid,
				low:      low,
				high:     high,
				size:     c.size,
				seqMin:   c.seqMin,
				seqMax:   c.seqMax,
			}
			next[key] = dst
		}
		dst.bid += c.bid
		dst.ask += c.ask
		dst.trade += c.trade
		if c.seqMin < dst.seqMin {
			dst.seqMin = c.seqMin
		}
		if c.seqMax > dst.seqMax {
			dst.seqMax = c.seqMax
		}
		dst.samples += c.samples
	}
	ws.cells = next
	return true
}

// estimateHeatmapPayloadSize returns a rough byte count for the serialized
// JSON artifact.  Uses O(1) arithmetic instead of json.Marshal to avoid
// hot-path allocations.  Constants are intentionally conservative (over-
// estimate) so that the budget is never exceeded.
//
// Each HeatmapCellV1 JSON object ≈ 280 bytes (9 key/value pairs including
// float64 numbers up to ~15 chars, two int64 fields, one short string).
// Artifact envelope (venue, instrument, timeframe, window bounds, cells array
// wrapper) ≈ 200 bytes.
func estimateHeatmapPayloadSize(cellCount int) int {
	const (
		overheadBytes = 200 // artifact envelope fields + JSON structure
		cellBytes     = 280 // conservative upper bound per cell
	)
	return overheadBytes + cellCount*cellBytes
}

func (uc *BuildHeatmap) trimToPayloadBudget(a domain.HeatmapArtifactV1) domain.HeatmapArtifactV1 {
	out := a
	if len(out.Cells) == 0 {
		return out
	}
	maxCells := (uc.cfg.MaxPayloadBytes - 200) / 280
	if maxCells < 1 {
		maxCells = 1
	}
	if len(out.Cells) > maxCells {
		out.Cells = pickTopNCells(out.Cells, maxCells)
	}
	return out
}

func toArtifact(req BuildHeatmapRequest, ws *windowState) domain.HeatmapArtifactV1 {
	cells := make([]domain.HeatmapCellV1, 0, len(ws.cells))
	for _, c := range ws.cells {
		cells = append(cells, domain.HeatmapCellV1{
			PriceBucketLow:  c.low,
			PriceBucketHigh: c.high,
			SizeBucket:      c.size,
			BidLiquidity:    c.bid,
			AskLiquidity:    c.ask,
			TradeVolume:     c.trade,
			SeqMin:          c.seqMin,
			SeqMax:          c.seqMax,
			Samples:         c.samples,
		})
	}
	slices.SortFunc(cells, domainCompareCellOrder)
	return domain.HeatmapArtifactV1{
		Venue:         naming.CanonicalVenue(req.Venue),
		Instrument:    naming.CanonicalInstrument(req.Instrument),
		Timeframe:     naming.NormalizeTimeframe(req.Timeframe),
		WindowStartTs: ws.windowStartMs,
		WindowEndTs:   ws.windowEndMs,
		Cells:         cells,
	}
}

func domainCompareCellOrder(a, b domain.HeatmapCellV1) int {
	if a.PriceBucketLow < b.PriceBucketLow {
		return -1
	}
	if a.PriceBucketLow > b.PriceBucketLow {
		return 1
	}
	if a.PriceBucketHigh < b.PriceBucketHigh {
		return -1
	}
	if a.PriceBucketHigh > b.PriceBucketHigh {
		return 1
	}
	as := strings.ToUpper(strings.TrimSpace(a.SizeBucket))
	bs := strings.ToUpper(strings.TrimSpace(b.SizeBucket))
	if as < bs {
		return -1
	}
	if as > bs {
		return 1
	}
	return 0
}

func validateHeatmapRequest(req BuildHeatmapRequest) *problem.Problem {
	return validation.Collect(
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
	)
}

func partitionKey(req BuildHeatmapRequest) string {
	return naming.CanonicalVenue(req.Venue) + "|" +
		naming.CanonicalInstrument(req.Instrument) + "|" +
		naming.NormalizeTimeframe(req.Timeframe)
}

func bucketIndex(price, binSize float64, mult int64) int64 {
	step := binSize * float64(mult)
	return int64(math.Floor(price / step))
}

func priceBounds(bucketIdx int64, binSize float64, mult int64) (float64, float64) {
	step := binSize * float64(mult)
	low := float64(bucketIdx) * step
	high := low + step
	return low, high
}

func makeCellKey(priceIdx int64, sizeBucket string) int64 {
	return priceIdx<<4 | int64(sizeToOrdinal(sizeBucket))
}

func sizeToOrdinal(s string) int {
	switch s {
	case "XS":
		return 0
	case "S":
		return 1
	case "M":
		return 2
	case "L":
		return 3
	case "XL":
		return 4
	default:
		return 5
	}
}

func toSizeBucket(size float64) string {
	switch {
	case size < 0.25:
		return "XS"
	case size < 1:
		return "S"
	case size < 5:
		return "M"
	case size < 20:
		return "L"
	default:
		return "XL"
	}
}

func applyEventVolume(c *heatmapCellState, req BuildHeatmapRequest) {
	eventType := naming.NormalizeEventType(req.EventType)
	side := naming.NormalizeSide(req.Side)
	switch eventType {
	case "marketdata.bookdelta":
		if side == "buy" {
			c.bid += req.Size
		} else {
			c.ask += req.Size
		}
	default:
		c.trade += req.Size
	}
}

func keepTopCells(cells map[int64]*heatmapCellState, n int) map[int64]*heatmapCellState {
	type row struct {
		key  int64
		cell *heatmapCellState
	}
	list := make([]row, 0, len(cells))
	for k, c := range cells {
		list = append(list, row{key: k, cell: c})
	}
	slices.SortFunc(list, func(a, b row) int {
		ia := a.cell.bid + a.cell.ask + a.cell.trade
		ib := b.cell.bid + b.cell.ask + b.cell.trade
		if ia > ib {
			return -1
		}
		if ia < ib {
			return 1
		}
		if a.cell.low < b.cell.low {
			return -1
		}
		if a.cell.low > b.cell.low {
			return 1
		}
		if a.cell.size < b.cell.size {
			return -1
		}
		if a.cell.size > b.cell.size {
			return 1
		}
		return 0
	})
	if n > len(list) {
		n = len(list)
	}
	out := make(map[int64]*heatmapCellState, n)
	for _, r := range list[:n] {
		out[r.key] = r.cell
	}
	return out
}

func pickTopNCells(cells []domain.HeatmapCellV1, n int) []domain.HeatmapCellV1 {
	if n >= len(cells) {
		return cells
	}
	out := slices.Clone(cells)
	slices.SortFunc(out, func(a, b domain.HeatmapCellV1) int {
		ia := a.BidLiquidity + a.AskLiquidity + a.TradeVolume
		ib := b.BidLiquidity + b.AskLiquidity + b.TradeVolume
		if ia > ib {
			return -1
		}
		if ia < ib {
			return 1
		}
		return domainCompareCellOrder(a, b)
	})
	out = out[:n]
	slices.SortFunc(out, domainCompareCellOrder)
	return out
}

func timeframeToWindowMs(tf string) int64 {
	tf = naming.NormalizeTimeframe(tf)
	switch tf {
	case "1s":
		return int64(time.Second / time.Millisecond)
	case "5s":
		return int64(5 * time.Second / time.Millisecond)
	case "10s":
		return int64(10 * time.Second / time.Millisecond)
	case "1m", "raw":
		return int64(time.Minute / time.Millisecond)
	case "5m":
		return int64(5 * time.Minute / time.Millisecond)
	case "15m":
		return int64(15 * time.Minute / time.Millisecond)
	case "30m":
		return int64(30 * time.Minute / time.Millisecond)
	case "1h":
		return int64(time.Hour / time.Millisecond)
	case "4h":
		return int64(4 * time.Hour / time.Millisecond)
	case "1d":
		return int64(24 * time.Hour / time.Millisecond)
	default:
		return heatmapDefaultWindowMs
	}
}

func (uc *BuildHeatmap) priceBucketCount(ws *windowState) int {
	seen := make(map[float64]struct{}, len(ws.cells))
	for _, c := range ws.cells {
		seen[c.low] = struct{}{}
	}
	return len(seen)
}
