package contracts_test

import (
	"testing"

	"github.com/market-raccoon/internal/shared/contracts"
)

func TestProtoRolloutEnabledForEventType(t *testing.T) {
	t.Setenv(contracts.EnvProtoMarketDataTrade, "true")
	t.Setenv(contracts.EnvProtoMarketDataBookDelta, "1")
	t.Setenv(contracts.EnvProtoMarketDataMarkPrice, "yes")

	if !contracts.ProtoRolloutEnabledForEventType("marketdata.trade") {
		t.Fatal("expected trade proto rollout enabled")
	}
	if !contracts.ProtoRolloutEnabledForEventType("marketdata.bookdelta") {
		t.Fatal("expected bookdelta proto rollout enabled")
	}
	if !contracts.ProtoRolloutEnabledForEventType("marketdata.markprice") {
		t.Fatal("expected markprice proto rollout enabled")
	}
}

func TestProtoRolloutEnabledForEventType_DefaultDisabled(t *testing.T) {
	t.Setenv(contracts.EnvProtoMarketDataTrade, "")
	t.Setenv(contracts.EnvProtoMarketDataBookDelta, "")
	t.Setenv(contracts.EnvProtoMarketDataMarkPrice, "")

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
}
