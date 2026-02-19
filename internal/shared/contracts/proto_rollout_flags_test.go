package contracts_test

import (
	"testing"

	"github.com/market-raccoon/internal/shared/contracts"
)

func TestProtoRolloutEnabledForEventType(t *testing.T) {
	t.Setenv(contracts.EnvProtoMarketDataTrade, "true")
	t.Setenv(contracts.EnvProtoMarketDataBookDelta, "1")
	t.Setenv(contracts.EnvProtoMarketDataMarkPrice, "yes")
	t.Setenv(contracts.EnvProtoAggregationCandle, "on")
	t.Setenv(contracts.EnvProtoAggregationStats, "t")

	if !contracts.ProtoRolloutEnabledForEventType("marketdata.trade") {
		t.Fatal("expected trade proto rollout enabled")
	}
	if !contracts.ProtoRolloutEnabledForEventType("marketdata.bookdelta") {
		t.Fatal("expected bookdelta proto rollout enabled")
	}
	if !contracts.ProtoRolloutEnabledForEventType("marketdata.markprice") {
		t.Fatal("expected markprice proto rollout enabled")
	}
	if !contracts.ProtoRolloutEnabledForEventType("aggregation.candle") {
		t.Fatal("expected candle proto rollout enabled")
	}
	if !contracts.ProtoRolloutEnabledForEventType("aggregation.stats") {
		t.Fatal("expected stats proto rollout enabled")
	}
}

func TestProtoRolloutEnabledForEventType_DefaultDisabled(t *testing.T) {
	t.Setenv(contracts.EnvProtoMarketDataTrade, "")
	t.Setenv(contracts.EnvProtoMarketDataBookDelta, "")
	t.Setenv(contracts.EnvProtoMarketDataMarkPrice, "")
	t.Setenv(contracts.EnvProtoAggregationCandle, "")
	t.Setenv(contracts.EnvProtoAggregationStats, "")

	if contracts.ProtoRolloutEnabledForEventType("marketdata.trade") {
		t.Fatal("trade should be disabled by default")
	}
	if contracts.ProtoRolloutEnabledForEventType("marketdata.bookdelta") {
		t.Fatal("bookdelta should be disabled by default")
	}
	if contracts.ProtoRolloutEnabledForEventType("marketdata.markprice") {
		t.Fatal("markprice should be disabled by default")
	}
	if contracts.ProtoRolloutEnabledForEventType("marketdata.liquidation") {
		t.Fatal("unknown event types must remain disabled")
	}
	if contracts.ProtoRolloutEnabledForEventType("aggregation.candle") {
		t.Fatal("candle should be disabled by default")
	}
	if contracts.ProtoRolloutEnabledForEventType("aggregation.stats") {
		t.Fatal("stats should be disabled by default")
	}
}
