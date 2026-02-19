package coinbase_test

import (
	"encoding/json"
	"testing"

	"github.com/market-raccoon/internal/adapters/exchange/coinbase"
)

func TestBuildSubscriptions(t *testing.T) {
	msgs, p := coinbase.BuildSubscriptions([]string{"BTC-USD", "ethusd"})
	if p != nil {
		t.Fatalf("BuildSubscriptions: %v", p)
	}
	if len(msgs) != 1 {
		t.Fatalf("messages len=%d want 1", len(msgs))
	}

	var body map[string]any
	if err := json.Unmarshal(msgs[0], &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if body["type"] != "subscribe" {
		t.Fatalf("type=%v want subscribe", body["type"])
	}
	channels, ok := body["channels"].([]any)
	if !ok || len(channels) != 3 {
		t.Fatalf("channels=%v", body["channels"])
	}
}

func TestBuildSubscriptions_RequiresTickers(t *testing.T) {
	_, p := coinbase.BuildSubscriptions(nil)
	if p == nil {
		t.Fatal("expected problem")
	}
}

func TestBuildEndpoint(t *testing.T) {
	if got := coinbase.BuildEndpoint(""); got != coinbase.DefaultWSBaseURL {
		t.Fatalf("endpoint=%q want=%q", got, coinbase.DefaultWSBaseURL)
	}
	if got := coinbase.BuildEndpoint("wss://example.com/"); got != "wss://example.com" {
		t.Fatalf("endpoint=%q want=%q", got, "wss://example.com")
	}
}
