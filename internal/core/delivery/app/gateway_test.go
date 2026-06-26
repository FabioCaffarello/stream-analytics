package app

import "testing"

func TestMarketDataGateway_SubscribePublishMonotonicAndDedup(t *testing.T) {
	g := NewMarketDataGateway()
	stream, p := g.Subscribe(GatewayFilter{
		Venue:   "binance",
		Symbol:  "BTCUSDT",
		Channel: "trade",
	})
	if p != nil {
		t.Fatalf("subscribe: %v", p)
	}

	out, p := g.Publish(GatewayEvent{
		StreamID: stream.StreamID,
		Venue:    "binance",
		Symbol:   "BTCUSDT",
		Channel:  "trade",
		Seq:      10,
	})
	if p != nil {
		t.Fatalf("publish seq=10: %v", p)
	}
	if got, want := len(out), 1; got != want {
		t.Fatalf("matched streams=%d want=%d", got, want)
	}

	out, p = g.Publish(GatewayEvent{
		StreamID: stream.StreamID,
		Venue:    "binance",
		Symbol:   "BTCUSDT",
		Channel:  "trade",
		Seq:      10, // duplicate
	})
	if p != nil {
		t.Fatalf("publish duplicate: %v", p)
	}
	if len(out) != 0 {
		t.Fatalf("duplicate seq should be dropped, got matches=%d", len(out))
	}

	out, p = g.Publish(GatewayEvent{
		StreamID: stream.StreamID,
		Venue:    "binance",
		Symbol:   "BTCUSDT",
		Channel:  "trade",
		Seq:      9, // out-of-order
	})
	if p != nil {
		t.Fatalf("publish out-of-order: %v", p)
	}
	if len(out) != 0 {
		t.Fatalf("out-of-order seq should be dropped, got matches=%d", len(out))
	}

	snap, ok := g.Snapshot(stream.StreamID)
	if !ok {
		t.Fatal("expected snapshot")
	}
	if got, want := snap.Seq, int64(10); got != want {
		t.Fatalf("snapshot seq=%d want=%d", got, want)
	}
}

func TestMarketDataGateway_FilterMatch(t *testing.T) {
	g := NewMarketDataGateway()
	_, p := g.Subscribe(GatewayFilter{Venue: "binance", Symbol: "BTCUSDT", Channel: "book_delta", Depth: 20})
	if p != nil {
		t.Fatalf("subscribe: %v", p)
	}
	_, p = g.Subscribe(GatewayFilter{Venue: "bybit", Symbol: "BTCUSDT", Channel: "book_delta", Depth: 20})
	if p != nil {
		t.Fatalf("subscribe: %v", p)
	}

	out, p := g.Publish(GatewayEvent{
		Venue:   "binance",
		Symbol:  "BTCUSDT",
		Channel: "book_delta",
		Depth:   20,
		Seq:     1,
	})
	if p != nil {
		t.Fatalf("publish: %v", p)
	}
	if got, want := len(out), 1; got != want {
		t.Fatalf("matched streams=%d want=%d", got, want)
	}
	if got, want := out[0].Filter.Venue, "binance"; got != want {
		t.Fatalf("venue=%s want=%s", got, want)
	}
}
