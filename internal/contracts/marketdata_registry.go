package contracts

import (
	marketdomain "github.com/FabioCaffarello/stream-analytics/internal/core/marketdata/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/codec"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
	marketdatav1 "github.com/FabioCaffarello/stream-analytics/internal/shared/proto/gen/marketdata/v1"
)

const marketDataV1Version int32 = 1

type encoderDecoder interface {
	codec.Encoder
	codec.Decoder
}

// RegisterMarketDataV1 registers marketdata v1 payload codecs for JSON and protobuf.
func RegisterMarketDataV1(reg *codec.Registry) *problem.Problem {
	if reg == nil {
		return problem.New(problem.ValidationFailed, "codec registry must not be nil")
	}

	if p := registerDual(
		reg,
		"marketdata.trade",
		codec.JSONCodec[marketdomain.TradeTickV1]{},
		codec.ProtoCodec[*marketdatav1.TradeTickV1]{New: func() *marketdatav1.TradeTickV1 { return &marketdatav1.TradeTickV1{} }},
	); p != nil {
		return p
	}
	if p := registerDual(
		reg,
		"marketdata.bookdelta",
		codec.JSONCodec[marketdomain.BookDeltaV1]{},
		codec.ProtoCodec[*marketdatav1.BookDeltaV1]{New: func() *marketdatav1.BookDeltaV1 { return &marketdatav1.BookDeltaV1{} }},
	); p != nil {
		return p
	}
	if p := registerDual(
		reg,
		"marketdata.markprice",
		codec.JSONCodec[marketdomain.MarkPriceTickV1]{},
		codec.ProtoCodec[*marketdatav1.MarkPriceTickV1]{New: func() *marketdatav1.MarkPriceTickV1 { return &marketdatav1.MarkPriceTickV1{} }},
	); p != nil {
		return p
	}
	if p := registerDual(
		reg,
		"marketdata.liquidation",
		codec.JSONCodec[marketdomain.LiquidationTickV1]{},
		codec.ProtoCodec[*marketdatav1.LiquidationTickV1]{New: func() *marketdatav1.LiquidationTickV1 { return &marketdatav1.LiquidationTickV1{} }},
	); p != nil {
		return p
	}
	if p := registerDual(
		reg,
		"marketdata.open_interest",
		codec.JSONCodec[marketdomain.OpenInterestTickV1]{},
		codec.ProtoCodec[*marketdatav1.OpenInterestTickV1]{New: func() *marketdatav1.OpenInterestTickV1 { return &marketdatav1.OpenInterestTickV1{} }},
	); p != nil {
		return p
	}

	return nil
}

func registerDual(reg *codec.Registry, eventType string, jsonCodec encoderDecoder, protoCodec encoderDecoder) *problem.Problem {
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
