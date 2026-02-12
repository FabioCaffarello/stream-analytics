package contracts

import (
	"reflect"

	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	marketdatav1 "github.com/market-raccoon/internal/shared/proto/gen/marketdata/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type authorityBinding struct {
	EventType string
	Version   int32

	// MessageName is the fully qualified protobuf message name.
	MessageName   string
	RegistryProto string
	NewProto      func() protoreflect.ProtoMessage
	DomainType    reflect.Type

	// ProtoToDomain and DomainToProto are mandatory contract boundary converters.
	ProtoToDomain func(protoreflect.ProtoMessage) any
	DomainToProto func(any) protoreflect.ProtoMessage

	// ProtoToDomainFieldMap captures source-of-truth field mapping from proto to domain.
	ProtoToDomainFieldMap map[string]string
	// IgnoredDomainFields documents exported domain fields intentionally excluded from mapping.
	IgnoredDomainFields []string
	// IgnoredProtoFields documents proto fields intentionally excluded from mapping.
	IgnoredProtoFields []string
}

// marketDataAuthorityBindings defines the contract authority model:
// protobuf schema fields are canonical, and domain fields are projections.
var marketDataAuthorityBindings = []authorityBinding{
	{
		EventType:     "marketdata.trade",
		Version:       marketDataV1Version,
		MessageName:   "marketdata.v1.TradeTickV1",
		RegistryProto: "marketdata/v1/trade.proto",
		NewProto:      func() protoreflect.ProtoMessage { return &marketdatav1.TradeTickV1{} },
		DomainType:    reflect.TypeOf(marketdomain.TradeTickV1{}),
		ProtoToDomain: func(msg protoreflect.ProtoMessage) any {
			return ProtoToDomainTradeTickV1(msg.(*marketdatav1.TradeTickV1))
		},
		DomainToProto: func(v any) protoreflect.ProtoMessage {
			return DomainToProtoTradeTickV1(v.(marketdomain.TradeTickV1))
		},
		ProtoToDomainFieldMap: map[string]string{
			"price":        "Price",
			"size":         "Size",
			"side":         "Side",
			"trade_id":     "TradeID",
			"timestamp_ms": "Timestamp",
		},
	},
	{
		EventType:     "marketdata.bookdelta",
		Version:       marketDataV1Version,
		MessageName:   "marketdata.v1.BookDeltaV1",
		RegistryProto: "marketdata/v1/bookdelta.proto",
		NewProto:      func() protoreflect.ProtoMessage { return &marketdatav1.BookDeltaV1{} },
		DomainType:    reflect.TypeOf(marketdomain.BookDeltaV1{}),
		ProtoToDomain: func(msg protoreflect.ProtoMessage) any {
			return ProtoToDomainBookDeltaV1(msg.(*marketdatav1.BookDeltaV1))
		},
		DomainToProto: func(v any) protoreflect.ProtoMessage {
			return DomainToProtoBookDeltaV1(v.(marketdomain.BookDeltaV1))
		},
		ProtoToDomainFieldMap: map[string]string{
			"bids":                 "Bids",
			"asks":                 "Asks",
			"first_update_id":      "FirstID",
			"final_update_id":      "FinalID",
			"prev_final_update_id": "PrevFinal",
			"timestamp_ms":         "Timestamp",
		},
	},
	{
		EventType:     "marketdata.markprice",
		Version:       marketDataV1Version,
		MessageName:   "marketdata.v1.MarkPriceTickV1",
		RegistryProto: "marketdata/v1/markprice.proto",
		NewProto:      func() protoreflect.ProtoMessage { return &marketdatav1.MarkPriceTickV1{} },
		DomainType:    reflect.TypeOf(marketdomain.MarkPriceTickV1{}),
		ProtoToDomain: func(msg protoreflect.ProtoMessage) any {
			return ProtoToDomainMarkPriceTickV1(msg.(*marketdatav1.MarkPriceTickV1))
		},
		DomainToProto: func(v any) protoreflect.ProtoMessage {
			return DomainToProtoMarkPriceTickV1(v.(marketdomain.MarkPriceTickV1))
		},
		ProtoToDomainFieldMap: map[string]string{
			"mark_price":   "MarkPrice",
			"index_price":  "IndexPrice",
			"funding_rate": "FundingRate",
			"timestamp_ms": "Timestamp",
		},
	},
	{
		EventType:     "marketdata.liquidation",
		Version:       marketDataV1Version,
		MessageName:   "marketdata.v1.LiquidationTickV1",
		RegistryProto: "marketdata/v1/liquidation.proto",
		NewProto:      func() protoreflect.ProtoMessage { return &marketdatav1.LiquidationTickV1{} },
		DomainType:    reflect.TypeOf(marketdomain.LiquidationTickV1{}),
		ProtoToDomain: func(msg protoreflect.ProtoMessage) any {
			return ProtoToDomainLiquidationTickV1(msg.(*marketdatav1.LiquidationTickV1))
		},
		DomainToProto: func(v any) protoreflect.ProtoMessage {
			return DomainToProtoLiquidationTickV1(v.(marketdomain.LiquidationTickV1))
		},
		ProtoToDomainFieldMap: map[string]string{
			"side":         "Side",
			"price":        "Price",
			"size":         "Size",
			"timestamp_ms": "Timestamp",
		},
	},
}
