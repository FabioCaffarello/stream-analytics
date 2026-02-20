package contracts_test

import (
	"testing"

	"github.com/market-raccoon/internal/shared/contracts"
)

func TestProtoRolloutEnabledForEventType(t *testing.T) {
	contracts.ResetProtoRolloutCache()
	t.Cleanup(contracts.ResetProtoRolloutCache)

	t.Setenv(contracts.EnvProtoMarketDataTrade, "true")
	t.Setenv(contracts.EnvProtoMarketDataBookDelta, "1")
	t.Setenv(contracts.EnvProtoMarketDataMarkPrice, "yes")
	t.Setenv(contracts.EnvProtoMarketDataLiquidation, "on")
	t.Setenv(contracts.EnvProtoAggregationCandle, "on")
	t.Setenv(contracts.EnvProtoAggregationStats, "t")
	t.Setenv(contracts.EnvProtoAggregationSnapshot, "1")
	t.Setenv(contracts.EnvProtoInsightsVPVR, "true")
	t.Setenv(contracts.EnvProtoInsightsHeatmap, "1")
	t.Setenv(contracts.EnvProtoInsightsCrossVenue, "yes")

	cases := []string{
		"marketdata.trade",
		"marketdata.bookdelta",
		"marketdata.markprice",
		"marketdata.liquidation",
		"aggregation.candle",
		"aggregation.stats",
		"aggregation.snapshot",
		"aggregation.orderbook_inconsistency",
		"insights.volume_profile_snapshot",
		"insights.volume_profile_delta",
		"insights.heatmap_snapshot",
		"insights.heatmap_delta",
		"insights.crossvenue.trade_snapshot",
		"insights.crossvenue.spread_signal",
	}
	for _, et := range cases {
		if !contracts.ProtoRolloutEnabledForEventType(et) {
			t.Fatalf("expected %s proto rollout enabled", et)
		}
	}
}

func TestProtoRolloutEnabledForEventType_DefaultDisabled(t *testing.T) {
	contracts.ResetProtoRolloutCache()
	t.Cleanup(contracts.ResetProtoRolloutCache)

	all := []string{
		contracts.EnvProtoMarketDataTrade,
		contracts.EnvProtoMarketDataBookDelta,
		contracts.EnvProtoMarketDataMarkPrice,
		contracts.EnvProtoMarketDataLiquidation,
		contracts.EnvProtoAggregationCandle,
		contracts.EnvProtoAggregationStats,
		contracts.EnvProtoAggregationSnapshot,
		contracts.EnvProtoInsightsVPVR,
		contracts.EnvProtoInsightsHeatmap,
		contracts.EnvProtoInsightsCrossVenue,
	}
	for _, env := range all {
		t.Setenv(env, "")
	}

	cases := []string{
		"marketdata.trade",
		"marketdata.bookdelta",
		"marketdata.markprice",
		"marketdata.liquidation",
		"aggregation.candle",
		"aggregation.stats",
		"aggregation.snapshot",
		"insights.volume_profile_snapshot",
		"insights.heatmap_snapshot",
		"insights.crossvenue.trade_snapshot",
		"totally.unknown.event",
	}
	for _, et := range cases {
		if contracts.ProtoRolloutEnabledForEventType(et) {
			t.Fatalf("%s should be disabled by default", et)
		}
	}
}

func TestProtoRolloutEnabledForEventType_ConfigOverridesEnv(t *testing.T) {
	contracts.ResetProtoRolloutCache()
	t.Cleanup(contracts.ResetProtoRolloutCache)

	t.Setenv(contracts.EnvProtoMarketDataTrade, "1")
	contracts.SetProtoRolloutConfig(map[string]bool{
		"marketdata.trade": false,
	})

	if contracts.ProtoRolloutEnabledForEventType("marketdata.trade") {
		t.Fatal("expected config override to disable marketdata.trade")
	}
}

func TestProtoRolloutEnabledForEventType_ConfigSupportsHotReload(t *testing.T) {
	contracts.ResetProtoRolloutCache()
	t.Cleanup(contracts.ResetProtoRolloutCache)

	contracts.SetProtoRolloutConfig(map[string]bool{
		"marketdata.trade": false,
	})
	if contracts.ProtoRolloutEnabledForEventType("marketdata.trade") {
		t.Fatal("expected initial config false")
	}

	contracts.SetProtoRolloutConfig(map[string]bool{
		"marketdata.trade": true,
	})
	if !contracts.ProtoRolloutEnabledForEventType(" marketdata.trade ") {
		t.Fatal("expected config hot-reload to enable marketdata.trade")
	}
}
