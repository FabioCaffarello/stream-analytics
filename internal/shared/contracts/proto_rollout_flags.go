package contracts

import (
	"os"
	"strings"
)

const (
	EnvProtoMarketDataTrade     = "PROTO_MARKETDATA_TRADE"
	EnvProtoMarketDataBookDelta = "PROTO_MARKETDATA_BOOKDELTA"
	EnvProtoMarketDataMarkPrice = "PROTO_MARKETDATA_MARKPRICE"
	EnvProtoAggregationCandle   = "PROTO_AGGREGATION_CANDLE"
	EnvProtoAggregationStats    = "PROTO_AGGREGATION_STATS"
)

// ProtoRolloutEnabledForEventType reports whether protobuf delivery is enabled
// for a specific event type via rollout environment flags.
func ProtoRolloutEnabledForEventType(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "marketdata.trade":
		return envBool(EnvProtoMarketDataTrade)
	case "marketdata.bookdelta":
		return envBool(EnvProtoMarketDataBookDelta)
	case "marketdata.markprice":
		return envBool(EnvProtoMarketDataMarkPrice)
	case "aggregation.candle":
		return envBool(EnvProtoAggregationCandle)
	case "aggregation.stats":
		return envBool(EnvProtoAggregationStats)
	default:
		return false
	}
}

func envBool(name string) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch raw {
	case "1", "true", "t", "yes", "y", "on":
		return true
	default:
		return false
	}
}
