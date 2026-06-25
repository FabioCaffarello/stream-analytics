package contracts

import (
	"os"
	"strings"
	"sync"
)

const (
	EnvProtoMarketDataTrade        = "PROTO_MARKETDATA_TRADE"
	EnvProtoMarketDataBookDelta    = "PROTO_MARKETDATA_BOOKDELTA"
	EnvProtoMarketDataMarkPrice    = "PROTO_MARKETDATA_MARKPRICE"
	EnvProtoMarketDataLiquidation  = "PROTO_MARKETDATA_LIQUIDATION"
	EnvProtoMarketDataOpenInterest = "PROTO_MARKETDATA_OPEN_INTEREST"
	EnvProtoAggregationCandle      = "PROTO_AGGREGATION_CANDLE"
	EnvProtoAggregationStats       = "PROTO_AGGREGATION_STATS"
	EnvProtoAggregationTape        = "PROTO_AGGREGATION_TAPE"
	EnvProtoAggregationOI          = "PROTO_AGGREGATION_OI"
	EnvProtoAggregationCVD         = "PROTO_AGGREGATION_CVD"
	EnvProtoAggregationDeltaVolume = "PROTO_AGGREGATION_DELTA_VOLUME"
	EnvProtoAggregationBarStats    = "PROTO_AGGREGATION_BAR_STATS"
	EnvProtoAggregationSnapshot    = "PROTO_AGGREGATION_SNAPSHOT"
	EnvProtoInsightsVPVR           = "PROTO_INSIGHTS_VPVR"
	EnvProtoInsightsHeatmap        = "PROTO_INSIGHTS_HEATMAP"
	EnvProtoInsightsCrossVenue     = "PROTO_INSIGHTS_CROSSVENUE"
)

// protoFlagCache holds cached rollout flag values, populated once at first access.
var (
	protoFlagOnce  sync.Once
	protoFlagCache map[string]bool
	protoFlagMu    sync.RWMutex
	protoFlagCfg   map[string]bool
)

// eventTypeToEnvVar maps each event type to its rollout environment variable.
// Multiple event types may share the same env var.
var eventTypeToEnvVar = map[string]string{
	"marketdata.trade":                    EnvProtoMarketDataTrade,
	"marketdata.bookdelta":                EnvProtoMarketDataBookDelta,
	"marketdata.markprice":                EnvProtoMarketDataMarkPrice,
	"marketdata.liquidation":              EnvProtoMarketDataLiquidation,
	"marketdata.open_interest":            EnvProtoMarketDataOpenInterest,
	"aggregation.candle":                  EnvProtoAggregationCandle,
	"aggregation.stats":                   EnvProtoAggregationStats,
	"aggregation.tape":                    EnvProtoAggregationTape,
	"aggregation.oi":                      EnvProtoAggregationOI,
	"aggregation.cvd":                     EnvProtoAggregationCVD,
	"aggregation.delta_volume":            EnvProtoAggregationDeltaVolume,
	"aggregation.bar_stats":               EnvProtoAggregationBarStats,
	"aggregation.snapshot":                EnvProtoAggregationSnapshot,
	"aggregation.orderbook_inconsistency": EnvProtoAggregationSnapshot,
	"insights.volume_profile_snapshot":    EnvProtoInsightsVPVR,
	"insights.volume_profile_delta":       EnvProtoInsightsVPVR,
	"insights.heatmap_snapshot":           EnvProtoInsightsHeatmap,
	"insights.heatmap_delta":              EnvProtoInsightsHeatmap,
	"insights.crossvenue.trade_snapshot":  EnvProtoInsightsCrossVenue,
	"insights.crossvenue.spread_signal":   EnvProtoInsightsCrossVenue,
}

func initProtoFlagCache() {
	protoFlagCache = make(map[string]bool, len(eventTypeToEnvVar))
	// Resolve each env var once and cache per event type.
	resolved := make(map[string]bool, 10)
	for eventType, envVar := range eventTypeToEnvVar {
		val, ok := resolved[envVar]
		if !ok {
			val = envBool(envVar)
			resolved[envVar] = val
		}
		protoFlagCache[eventType] = val
	}
}

// ProtoRolloutEnabledForEventType reports whether protobuf delivery is enabled
// for a specific event type.
//
// Precedence (highest to lowest):
//  1. Runtime config set via SetProtoRolloutConfig (from validated AppConfig).
//  2. Environment variables (read once at first access, cached for process lifetime).
//
// In production, config always takes precedence because SetProtoRolloutConfig
// is called during bootstrap after config validation.  Environment variables
// serve as a fallback for local development and ad-hoc testing where JSONC
// config files are not used.
func ProtoRolloutEnabledForEventType(eventType string) bool {
	eventType = strings.ToLower(strings.TrimSpace(eventType))

	protoFlagMu.RLock()
	cfgFlags := protoFlagCfg
	protoFlagMu.RUnlock()
	if cfgFlags != nil {
		return cfgFlags[eventType]
	}

	protoFlagOnce.Do(initProtoFlagCache)
	return protoFlagCache[eventType]
}

// SetProtoRolloutConfig sets runtime rollout flags from validated config.
// Calling this function switches rollout source from env vars to config.
func SetProtoRolloutConfig(flags map[string]bool) {
	next := make(map[string]bool, len(eventTypeToEnvVar))
	for eventType := range eventTypeToEnvVar {
		next[eventType] = flags[eventType]
	}
	protoFlagMu.Lock()
	protoFlagCfg = next
	protoFlagMu.Unlock()
}

// ResetProtoRolloutCache forces re-reading environment variables on the next
// call to ProtoRolloutEnabledForEventType. Intended for tests only.
func ResetProtoRolloutCache() {
	protoFlagOnce = sync.Once{}
	protoFlagCache = nil
	protoFlagMu.Lock()
	protoFlagCfg = nil
	protoFlagMu.Unlock()
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
