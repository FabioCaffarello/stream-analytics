package app

import (
	"context"
	"strings"

	"github.com/FabioCaffarello/stream-analytics/internal/core/marketdata/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/naming"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/result"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/validation"
)

const (
	markPriceEventType   = "marketdata.markprice"
	liquidationEventType = "marketdata.liquidation"
	defaultLMDedupWindow = 4096
)

type NormalizeMarkPriceLiquidationConfig struct {
	DedupWindowSize int
}

type NormalizeMarkPriceLiquidationRequest struct {
	Venue              string
	Instrument         string
	EventType          string
	Version            int
	TsExchange         int64
	TsIngest           int64
	SourceIdempotency  string
	MarkPricePayload   *domain.MarkPriceTickV1
	LiquidationPayload *domain.LiquidationTickV1
}

type NormalizeMarkPriceLiquidationResponse struct {
	Venue       string
	Instrument  string
	EventType   string
	Version     int
	DedupKey    string
	IsDuplicate bool
	MarkPrice   *domain.MarkPriceTickV1
	Liquidation *domain.LiquidationTickV1
}

type NormalizeMarkPriceLiquidation struct {
	dedupWindow int
	seen        map[string]struct{}
	seenOrd     []string
}

func NewNormalizeMarkPriceLiquidation(cfg NormalizeMarkPriceLiquidationConfig) *NormalizeMarkPriceLiquidation {
	if cfg.DedupWindowSize <= 0 {
		cfg.DedupWindowSize = defaultLMDedupWindow
	}
	return &NormalizeMarkPriceLiquidation{
		dedupWindow: cfg.DedupWindowSize,
		seen:        make(map[string]struct{}, cfg.DedupWindowSize),
		seenOrd:     make([]string, 0, cfg.DedupWindowSize),
	}
}

func (uc *NormalizeMarkPriceLiquidation) Execute(_ context.Context, req NormalizeMarkPriceLiquidationRequest) result.Result[NormalizeMarkPriceLiquidationResponse] {
	if p := validation.Collect(
		validation.NonEmptyString("venue", req.Venue),
		validation.NonEmptyString("instrument", req.Instrument),
		validation.NonEmptyString("event_type", req.EventType),
		validation.PositiveInt("version", int64(req.Version)),
		validation.PositiveInt("ts_ingest", req.TsIngest),
	); p != nil {
		return result.FailProblem[NormalizeMarkPriceLiquidationResponse](p)
	}

	eventType := naming.NormalizeEventType(req.EventType)
	if eventType != markPriceEventType && eventType != liquidationEventType {
		return result.FailProblem[NormalizeMarkPriceLiquidationResponse](
			problem.Newf(problem.ValidationFailed, "unsupported event_type %q", req.EventType),
		)
	}

	venue := naming.CanonicalVenue(req.Venue)
	instrument := naming.CanonicalInstrument(req.Instrument)

	resp := NormalizeMarkPriceLiquidationResponse{
		Venue:      venue,
		Instrument: instrument,
		EventType:  eventType,
		Version:    req.Version,
	}

	switch eventType {
	case markPriceEventType:
		if req.MarkPricePayload == nil {
			return result.FailProblem[NormalizeMarkPriceLiquidationResponse](
				problem.New(problem.ValidationFailed, "markprice payload must not be nil"),
			)
		}
		norm := *req.MarkPricePayload
		resp.MarkPrice = &norm
	case liquidationEventType:
		if req.LiquidationPayload == nil {
			return result.FailProblem[NormalizeMarkPriceLiquidationResponse](
				problem.New(problem.ValidationFailed, "liquidation payload must not be nil"),
			)
		}
		norm := *req.LiquidationPayload
		norm.Side = strings.ToLower(strings.TrimSpace(norm.Side))
		resp.Liquidation = &norm
	}

	dedupKey := buildLMDedupKey(resp, req)
	resp.DedupKey = dedupKey

	if _, ok := uc.seen[dedupKey]; ok {
		resp.IsDuplicate = true
		return result.Ok(resp)
	}

	uc.recordSeen(dedupKey)
	return result.Ok(resp)
}

func buildLMDedupKey(resp NormalizeMarkPriceLiquidationResponse, req NormalizeMarkPriceLiquidationRequest) string {
	switch resp.EventType {
	case markPriceEventType:
		return BuildMarkPriceDedupKey(
			resp.Venue,
			resp.Instrument,
			resp.Version,
			req.TsExchange,
			req.TsIngest,
			*resp.MarkPrice,
			req.SourceIdempotency,
		)
	case liquidationEventType:
		return BuildLiquidationDedupKey(
			resp.Venue,
			resp.Instrument,
			resp.Version,
			req.TsExchange,
			req.TsIngest,
			*resp.Liquidation,
			req.SourceIdempotency,
		)
	default:
		return ""
	}
}

func (uc *NormalizeMarkPriceLiquidation) recordSeen(key string) {
	if uc.dedupWindow <= 0 {
		return
	}
	if len(uc.seenOrd) >= uc.dedupWindow {
		oldest := uc.seenOrd[0]
		uc.seenOrd = uc.seenOrd[1:]
		delete(uc.seen, oldest)
	}
	uc.seen[key] = struct{}{}
	uc.seenOrd = append(uc.seenOrd, key)
}
