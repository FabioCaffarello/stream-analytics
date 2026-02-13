package app

import (
	"context"
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/market-raccoon/internal/core/insights/domain"
	"github.com/market-raccoon/internal/shared/hash"
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

var heatmapSizeBuckets = []string{"XS", "S", "M", "L", "XL"}

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
	cells         map[string]*heatmapCellState
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

	artifact := toArtifact(req, ws)
	trimmed := uc.trimToPayloadBudget(artifact)
	if p := trimmed.Validate(); p != nil {
		return result.FailProblem[BuildHeatmapResponse](p)
	}

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
	return hash.HashFields(
		naming.CanonicalVenue(a.Venue),
		naming.CanonicalInstrument(a.Instrument),
		strings.ToLower(strings.TrimSpace(a.Timeframe)),
		strconv.FormatInt(a.WindowStartTs, 10),
		formatFloat(last.PriceBucketLow),
		formatFloat(last.PriceBucketHigh),
		strings.ToUpper(strings.TrimSpace(last.SizeBucket)),
		strconv.FormatInt(last.SeqMax, 10),
	)
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
		cells:         make(map[string]*heatmapCellState),
	}
	ps.windows[start] = ws
	ps.order = append(ps.order, start)
	slices.Sort(ps.order)
	return ws
}

func (uc *BuildHeatmap) apply(ws *windowState, req BuildHeatmapRequest) string {
	dropReason := ""
	for {
		priceIdx := bucketIndex(req.Price, req.TickSize, ws.priceMult)
		low, high := priceBounds(priceIdx, req.TickSize, ws.priceMult)
		sizeBucket := toSizeBucket(req.Size)
		cellKey := makeCellKey(priceIdx, sizeBucket)
		if _, exists := ws.cells[cellKey]; !exists {
			if uc.priceBucketCount(ws) >= uc.cfg.MaxPriceBucketsPerWindow {
				if uc.coarsen(ws, req.TickSize) {
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

func (uc *BuildHeatmap) coarsen(ws *windowState, tickSize float64) bool {
	if ws.priceMult >= 64 {
		return false
	}
	ws.priceMult *= 2
	next := make(map[string]*heatmapCellState, len(ws.cells))
	for _, c := range ws.cells {
		priceIdx := bucketIndex(c.priceMid, tickSize, ws.priceMult)
		low, high := priceBounds(priceIdx, tickSize, ws.priceMult)
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

func (uc *BuildHeatmap) trimToPayloadBudget(a domain.HeatmapArtifactV1) domain.HeatmapArtifactV1 {
	out := a
	if len(out.Cells) == 0 {
		return out
	}
	for len(out.Cells) > 1 {
		raw, _ := json.Marshal(out)
		if len(raw) <= uc.cfg.MaxPayloadBytes {
			return out
		}
		limit := len(out.Cells) * 9 / 10
		if limit == len(out.Cells) {
			limit--
		}
		if limit < 1 {
			limit = 1
		}
		out.Cells = pickTopNCells(out.Cells, limit)
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
		Timeframe:     strings.ToLower(strings.TrimSpace(req.Timeframe)),
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
		strings.ToLower(strings.TrimSpace(req.Timeframe))
}

func bucketIndex(price, tick float64, mult int64) int64 {
	step := tick * float64(mult)
	return int64(math.Floor(price / step))
}

func priceBounds(bucketIdx int64, tick float64, mult int64) (float64, float64) {
	step := tick * float64(mult)
	low := float64(bucketIdx) * step
	high := low + step
	return low, high
}

func makeCellKey(priceIdx int64, sizeBucket string) string {
	return strconv.FormatInt(priceIdx, 10) + "|" + strings.ToUpper(strings.TrimSpace(sizeBucket))
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
	eventType := strings.ToLower(strings.TrimSpace(req.EventType))
	side := strings.ToLower(strings.TrimSpace(req.Side))
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

func keepTopCells(cells map[string]*heatmapCellState, n int) map[string]*heatmapCellState {
	type row struct {
		key  string
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
	out := make(map[string]*heatmapCellState, n)
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
	tf = strings.ToLower(strings.TrimSpace(tf))
	switch tf {
	case "1s":
		return int64(time.Second / time.Millisecond)
	case "10s":
		return int64(10 * time.Second / time.Millisecond)
	case "1m", "raw":
		return int64(time.Minute / time.Millisecond)
	case "5m":
		return int64(5 * time.Minute / time.Millisecond)
	case "15m":
		return int64(15 * time.Minute / time.Millisecond)
	case "1h":
		return int64(time.Hour / time.Millisecond)
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

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
