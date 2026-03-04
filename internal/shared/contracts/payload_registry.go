package contracts

import (
	"fmt"
	"sync"

	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
	aggregationv1 "github.com/market-raccoon/internal/shared/proto/gen/aggregation/v1"
	aggregationv2 "github.com/market-raccoon/internal/shared/proto/gen/aggregation/v2"
	marketdatav1 "github.com/market-raccoon/internal/shared/proto/gen/marketdata/v1"
	"google.golang.org/protobuf/proto"
)

var (
	payloadRegistryOnce sync.Once
	payloadRegistryErr  *problem.Problem
)

const (
	marketDataEventTypeTrade          = "marketdata.trade"
	marketDataEventTypeBookDelta      = "marketdata.bookdelta"
	marketDataEventTypeMarkPrice      = "marketdata.markprice"
	marketDataEventTypeLiq            = "marketdata.liquidation"
	aggregationEventTypeCandle        = "aggregation.candle"
	aggregationEventTypeStats         = "aggregation.stats"
	aggregationEventTypeSnapshot      = "aggregation.snapshot"
	aggregationEventTypeInconsistency    = "aggregation.orderbook_inconsistency"
	aggregationEventTypeCrossVenueBook   = "aggregation.cross_venue_book"
)

type PayloadRegistryOptions struct {
	EnableInsightsVolumeProfileSnapshotProto bool
	EnableInsightsHeatmapSnapshotProto       bool
}

// BootstrapPayloadCodecRegistry configures shared codec payload encode/decode registry.
func BootstrapPayloadCodecRegistry() *problem.Problem {
	return BootstrapPayloadCodecRegistryWithOptions(PayloadRegistryOptions{})
}

// BootstrapPayloadCodecRegistryWithOptions configures shared codec payload
// encode/decode registry with explicit feature-flag options.
func BootstrapPayloadCodecRegistryWithOptions(opts PayloadRegistryOptions) *problem.Problem {
	payloadRegistryOnce.Do(func() {
		reg := codec.NewRegistry()
		if p := RegisterMarketDataPayloadV1(reg); p != nil {
			payloadRegistryErr = p
			return
		}
		if p := RegisterInsightsPayloadV1WithOptions(reg, InsightsCodecOptions{
			EnableVolumeProfileSnapshotProto: opts.EnableInsightsVolumeProfileSnapshotProto,
			EnableHeatmapSnapshotProto:       opts.EnableInsightsHeatmapSnapshotProto,
		}); p != nil {
			payloadRegistryErr = p
			return
		}
		if p := RegisterAggregationPayloadV1(reg); p != nil {
			payloadRegistryErr = p
			return
		}
		if p := RegisterEvidencePayloadV1(reg); p != nil {
			payloadRegistryErr = p
			return
		}
		if p := RegisterSignalsPayloadV1(reg); p != nil {
			payloadRegistryErr = p
			return
		}
		payloadRegistryErr = codec.SetPayloadRegistry(reg)
		if payloadRegistryErr == nil {
			metrics.SetCodecRegistrySize(reg.Size())
		}
	})
	return payloadRegistryErr
}

// RegisterMarketDataPayloadV1 registers domain-level payload codecs for runtime
// payload encoding/decoding across JSON and protobuf content types.
func RegisterMarketDataPayloadV1(reg *codec.Registry) *problem.Problem {
	if reg == nil {
		return problem.New(problem.ValidationFailed, "codec registry must not be nil")
	}

	if p := registerPayloadDual(
		reg,
		marketDataEventTypeTrade,
		codec.JSONCodec[marketdomain.TradeTickV1]{},
		domainProtoPayloadCodec[marketdomain.TradeTickV1, *marketdatav1.TradeTickV1]{
			newProto: func() *marketdatav1.TradeTickV1 { return &marketdatav1.TradeTickV1{} },
			toProto:  DomainToProtoTradeTickV1,
			toDomain: ProtoToDomainTradeTickV1,
		},
	); p != nil {
		return p
	}
	if p := registerPayloadDual(
		reg,
		marketDataEventTypeBookDelta,
		codec.JSONCodec[marketdomain.BookDeltaV1]{},
		domainProtoPayloadCodec[marketdomain.BookDeltaV1, *marketdatav1.BookDeltaV1]{
			newProto: func() *marketdatav1.BookDeltaV1 { return &marketdatav1.BookDeltaV1{} },
			toProto:  DomainToProtoBookDeltaV1,
			toDomain: ProtoToDomainBookDeltaV1,
		},
	); p != nil {
		return p
	}
	if p := registerPayloadDual(
		reg,
		marketDataEventTypeMarkPrice,
		codec.JSONCodec[marketdomain.MarkPriceTickV1]{},
		domainProtoPayloadCodec[marketdomain.MarkPriceTickV1, *marketdatav1.MarkPriceTickV1]{
			newProto: func() *marketdatav1.MarkPriceTickV1 { return &marketdatav1.MarkPriceTickV1{} },
			toProto:  DomainToProtoMarkPriceTickV1,
			toDomain: ProtoToDomainMarkPriceTickV1,
		},
	); p != nil {
		return p
	}
	if p := registerPayloadDual(
		reg,
		marketDataEventTypeLiq,
		codec.JSONCodec[marketdomain.LiquidationTickV1]{},
		domainProtoPayloadCodec[marketdomain.LiquidationTickV1, *marketdatav1.LiquidationTickV1]{
			newProto: func() *marketdatav1.LiquidationTickV1 { return &marketdatav1.LiquidationTickV1{} },
			toProto:  DomainToProtoLiquidationTickV1,
			toDomain: ProtoToDomainLiquidationTickV1,
		},
	); p != nil {
		return p
	}
	return nil
}

// RegisterAggregationPayloadV1 registers aggregation payload codecs for runtime
// envelope encoding/decoding across JSON and protobuf content types.
func RegisterAggregationPayloadV1(reg *codec.Registry) *problem.Problem {
	if reg == nil {
		return problem.New(problem.ValidationFailed, "codec registry must not be nil")
	}
	if p := registerPayloadDual(
		reg,
		aggregationEventTypeCandle,
		codec.JSONCodec[AggregationCandleClosedV1]{},
		domainProtoPayloadCodec[AggregationCandleClosedV1, *aggregationv1.CandleClosedV1]{
			newProto: func() *aggregationv1.CandleClosedV1 { return &aggregationv1.CandleClosedV1{} },
			toProto:  WireDTOToProtoCandleClosedV1,
			toDomain: ProtoToWireDTOCandleClosedV1,
		},
	); p != nil {
		return p
	}
	if p := registerPayloadDual(
		reg,
		aggregationEventTypeStats,
		codec.JSONCodec[AggregationStatsWindowClosedV1]{},
		domainProtoPayloadCodec[AggregationStatsWindowClosedV1, *aggregationv1.StatsWindowClosedV1]{
			newProto: func() *aggregationv1.StatsWindowClosedV1 { return &aggregationv1.StatsWindowClosedV1{} },
			toProto:  WireDTOToProtoStatsWindowClosedV1,
			toDomain: ProtoToWireDTOStatsWindowClosedV1,
		},
	); p != nil {
		return p
	}
	if p := registerPayloadDual(
		reg,
		aggregationEventTypeSnapshot,
		codec.JSONCodec[AggregationSnapshotV1]{},
		domainProtoPayloadCodec[AggregationSnapshotV1, *aggregationv1.OrderBookSnapshotV1]{
			newProto: func() *aggregationv1.OrderBookSnapshotV1 { return &aggregationv1.OrderBookSnapshotV1{} },
			toProto:  WireDTOToProtoSnapshotV1,
			toDomain: ProtoToWireDTOSnapshotV1,
		},
	); p != nil {
		return p
	}
	if p := registerPayloadDual(
		reg,
		aggregationEventTypeInconsistency,
		codec.JSONCodec[AggregationOrderBookInconsistencyV1]{},
		domainProtoPayloadCodec[AggregationOrderBookInconsistencyV1, *aggregationv1.OrderBookInconsistencyV1]{
			newProto: func() *aggregationv1.OrderBookInconsistencyV1 { return &aggregationv1.OrderBookInconsistencyV1{} },
			toProto:  WireDTOToProtoInconsistencyV1,
			toDomain: ProtoToWireDTOInconsistencyV1,
		},
	); p != nil {
		return p
	}
	if p := registerPayloadDual(
		reg,
		aggregationEventTypeCrossVenueBook,
		codec.JSONCodec[AggregationCrossVenueBookSnapshotV1]{},
		domainProtoPayloadCodec[AggregationCrossVenueBookSnapshotV1, *aggregationv2.CrossVenueBookSnapshotV1]{
			newProto: func() *aggregationv2.CrossVenueBookSnapshotV1 { return &aggregationv2.CrossVenueBookSnapshotV1{} },
			toProto:  WireDTOToProtoCrossVenueBookSnapshotV1,
			toDomain: ProtoToWireDTOCrossVenueBookSnapshotV1,
		},
	); p != nil {
		return p
	}
	return nil
}

func registerPayloadDual(reg *codec.Registry, eventType string, jsonCodec encoderDecoder, protoCodec encoderDecoder) *problem.Problem {
	if p := reg.Register(codec.SchemaKey{
		Type:    eventType,
		Version: marketDataV1Version,
		Format:  codec.FormatJSON,
	}, jsonCodec, jsonCodec); p != nil {
		return p
	}
	if p := reg.Register(codec.SchemaKey{
		Type:    eventType,
		Version: marketDataV1Version,
		Format:  codec.FormatProto,
	}, protoCodec, protoCodec); p != nil {
		return p
	}
	return nil
}

type domainProtoPayloadCodec[D any, P proto.Message] struct {
	newProto func() P
	toProto  func(D) P
	toDomain func(P) D
}

func (c domainProtoPayloadCodec[D, P]) Encode(v any) ([]byte, *problem.Problem) {
	if c.newProto == nil {
		return nil, problem.New(problem.ValidationFailed, "proto payload codec factory must not be nil")
	}
	if c.toProto == nil {
		return nil, problem.New(problem.ValidationFailed, "proto payload codec toProto converter must not be nil")
	}
	typedDomain, ok := v.(D)
	if !ok {
		var zeroD D
		return nil, problem.WithDetail(
			problem.Newf(problem.ValidationFailed, "proto payload type mismatch: got %T want %T", v, zeroD),
			"payload_type", fmt.Sprintf("%T", v),
		)
	}
	inner := codec.ProtoCodec[P]{New: c.newProto}
	return inner.Encode(c.toProto(typedDomain))
}

func (c domainProtoPayloadCodec[D, P]) Decode(b []byte) (any, *problem.Problem) {
	if c.newProto == nil {
		return nil, problem.New(problem.ValidationFailed, "proto payload codec factory must not be nil")
	}
	if c.toDomain == nil {
		return nil, problem.New(problem.ValidationFailed, "proto payload codec toDomain converter must not be nil")
	}
	inner := codec.ProtoCodec[P]{New: c.newProto}
	out, p := inner.Decode(b)
	if p != nil {
		return nil, p
	}
	typedProto, ok := out.(P)
	if !ok {
		var zeroP P
		return nil, problem.WithDetail(
			problem.Newf(problem.Internal, "proto payload decode type mismatch: got %T want %T", out, zeroP),
			"decoded_type", fmt.Sprintf("%T", out),
		)
	}
	return c.toDomain(typedProto), nil
}
