package hyperliquid

import (
	"strings"
	"testing"
)

func TestBuildSubscriptions(t *testing.T) {
	msgs, p := BuildSubscriptions([]string{"BTCUSDT", "ETHPERP"})
	if p != nil {
		t.Fatalf("BuildSubscriptions: %v", p)
	}
	if len(msgs) != 4 {
		t.Fatalf("messages len=%d want 4", len(msgs))
	}
	joined := string(msgs[0]) + string(msgs[1]) + string(msgs[2]) + string(msgs[3])
	if !strings.Contains(joined, `"type":"trades"`) || !strings.Contains(joined, `"type":"l2Book"`) {
		t.Fatalf("unexpected body: %s", joined)
	}
}

func TestBuildSubscriptions_RequiresTickers(t *testing.T) {
	_, p := BuildSubscriptions(nil)
	if p == nil {
		t.Fatal("expected problem")
	}
}

func TestToCoinName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "BTCUSDT", want: "BTC"},
		{in: "ETHPERP", want: "ETH"},
		{in: "BTC", want: "BTC"},
		{in: "ETH-USD", want: "ETH"},
	}
	for _, tc := range tests {
		if got := toCoinName(tc.in); got != tc.want {
			t.Fatalf("toCoinName(%q)=%q want=%q", tc.in, got, tc.want)
		}
	}
}
