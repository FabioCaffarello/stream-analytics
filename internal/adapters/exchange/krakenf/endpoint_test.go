package krakenf_test

import (
	"encoding/json"
	"testing"

	"github.com/market-raccoon/internal/adapters/exchange/krakenf"
)

func TestBuildEndpoint(t *testing.T) {
	if got := krakenf.BuildEndpoint(""); got != krakenf.DefaultWSBaseURL {
		t.Fatalf("endpoint=%q want=%q", got, krakenf.DefaultWSBaseURL)
	}
	if got := krakenf.BuildEndpoint("wss://example.com/ws/"); got != "wss://example.com/ws" {
		t.Fatalf("endpoint=%q want=%q", got, "wss://example.com/ws")
	}
}

func TestBuildSubscriptions(t *testing.T) {
	msgs, p := krakenf.BuildSubscriptions([]string{"BTC-USD"})
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
		if body["event"] != "subscribe" {
			t.Fatalf("event[%d]=%v want subscribe", i, body["event"])
		}
		if _, ok := body["feed"].(string); !ok {
			t.Fatalf("feed[%d]=%T", i, body["feed"])
		}
		ids, ok := body["product_ids"].([]any)
		if !ok || len(ids) != 1 {
			t.Fatalf("product_ids[%d]=%v", i, body["product_ids"])
		}
	}
}

func TestBuildSubscriptions_RequiresTickers(t *testing.T) {
	_, p := krakenf.BuildSubscriptions(nil)
	if p == nil {
		t.Fatal("expected problem")
	}
}

func TestBuildSubscriptions_InvalidTicker(t *testing.T) {
	_, p := krakenf.BuildSubscriptions([]string{"INVALID"})
	if p == nil {
		t.Fatal("expected problem")
	}
}
