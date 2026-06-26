package kraken_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/adapters/exchange/kraken"
)

func TestBuildEndpoint(t *testing.T) {
	if got := kraken.BuildEndpoint(""); got != kraken.DefaultWSBaseURL {
		t.Fatalf("endpoint=%q want=%q", got, kraken.DefaultWSBaseURL)
	}
	if got := kraken.BuildEndpoint("wss://example.com/ws/"); got != "wss://example.com/ws" {
		t.Fatalf("endpoint=%q want=%q", got, "wss://example.com/ws")
	}
}

func TestBuildSubscriptions(t *testing.T) {
	msgs, p := kraken.BuildSubscriptions([]string{"BTC-USD", "eth/usdt"})
	if p != nil {
		t.Fatalf("BuildSubscriptions: %v", p)
	}
	if len(msgs) != 3 {
		t.Fatalf("messages len=%d want 3", len(msgs))
	}

	for i, msg := range msgs {
		var body map[string]any
		if err := json.Unmarshal(msg, &body); err != nil {
			t.Fatalf("json.Unmarshal[%d]: %v", i, err)
		}
		if body["method"] != "subscribe" {
			t.Fatalf("method[%d]=%v want subscribe", i, body["method"])
		}
		params, ok := body["params"].(map[string]any)
		if !ok {
			t.Fatalf("params[%d]=%T", i, body["params"])
		}
		symbols, ok := params["symbol"].([]any)
		if !ok || len(symbols) != 2 {
			t.Fatalf("params.symbol[%d]=%v", i, params["symbol"])
		}
	}

	if !strings.Contains(string(msgs[0]), "BTC/USD") {
		t.Fatalf("unexpected subscription body: %s", string(msgs[0]))
	}
}

func TestBuildSubscriptions_RequiresTickers(t *testing.T) {
	_, p := kraken.BuildSubscriptions(nil)
	if p == nil {
		t.Fatal("expected problem")
	}
}

func TestBuildSubscriptions_InvalidTicker(t *testing.T) {
	_, p := kraken.BuildSubscriptions([]string{"INVALID"})
	if p == nil {
		t.Fatal("expected problem")
	}
}
