package contracts

import (
	"os"
	"strings"
)

const (
	EnvProtoMarketDataTrade       = "PROTO_MARKETDATA_TRADE"
	EnvProtoMarketDataBookDelta   = "PROTO_MARKETDATA_BOOKDELTA"
	EnvProtoMarketDataMarkPrice   = "PROTO_MARKETDATA_MARKPRICE"
	EnvProtoMarketDataLiquidation = "PROTO_MARKETDATA_LIQUIDATION"
	EnvProtoAggregationCandle     = "PROTO_AGGREGATION_CANDLE"
	EnvProtoAggregationStats      = "PROTO_AGGREGATION_STATS"
	EnvProtoAggregationSnapshot   = "PROTO_AGGREGATION_SNAPSHOT"
	EnvProtoInsightsVPVR          = "PROTO_INSIGHTS_VPVR"
	EnvProtoInsightsHeatmap       = "PROTO_INSIGHTS_HEATMAP"
	EnvProtoInsightsCrossVenue    = "PROTO_INSIGHTS_CROSSVENUE"
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
	case "marketdata.liquidation":
		return envBool(EnvProtoMarketDataLiquidation)
	case "aggregation.candle":
		return envBool(EnvProtoAggregationCandle)
	case "aggregation.stats":
		return envBool(EnvProtoAggregationStats)
	case "aggregation.snapshot", "aggregation.orderbook_inconsistency":
		return envBool(EnvProtoAggregationSnapshot)
	case "insights.volume_profile_snapshot", "insights.volume_profile_delta":
		return envBool(EnvProtoInsightsVPVR)
	case "insights.heatmap_snapshot", "insights.heatmap_delta":
		return envBool(EnvProtoInsightsHeatmap)
	case "insights.crossvenue.trade_snapshot", "insights.crossvenue.spread_signal":
		return envBool(EnvProtoInsightsCrossVenue)
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
