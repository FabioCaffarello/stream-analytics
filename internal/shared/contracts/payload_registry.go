package contracts

import (
	"fmt"
	"sync"

	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/problem"
	marketdatav1 "github.com/market-raccoon/internal/shared/proto/gen/marketdata/v1"
	"google.golang.org/protobuf/proto"
)

var (
	payloadRegistryOnce sync.Once
	payloadRegistryErr  *problem.Problem
)

// BootstrapPayloadCodecRegistry configures shared codec payload encode/decode registry.
func BootstrapPayloadCodecRegistry() *problem.Problem {
	payloadRegistryOnce.Do(func() {
		reg := codec.NewRegistry()
		if p := RegisterMarketDataPayloadV1(reg); p != nil {
			payloadRegistryErr = p
			return
		}
		if p := RegisterInsightsPayloadV1(reg); p != nil {
			payloadRegistryErr = p
			return
		}
		payloadRegistryErr = codec.SetPayloadRegistry(reg)
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
		"marketdata.trade",
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
		"marketdata.bookdelta",
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
		"marketdata.markprice",
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
		"marketdata.liquidation",
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
