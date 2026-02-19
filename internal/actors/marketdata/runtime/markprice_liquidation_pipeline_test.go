package mdruntime

import (
	"slices"
	"testing"

	"github.com/market-raccoon/internal/actors/marketdata/ws"
)

func TestMarkPriceLiquidationReplayGolden(t *testing.T) {
	input := []*wsMessageFixture{
		{kind: "trade", n: 1},
		{kind: "liquidation", n: 10},
		{kind: "markprice", n: 100},
		{kind: "depth", n: 2},
		{kind: "trade", n: 3},
		{kind: "liquidation", n: 11},
		{kind: "markprice", n: 101},
		{kind: "depth", n: 4},
		{kind: "trade", n: 5},
		{kind: "liquidation", n: 12},
		{kind: "markprice", n: 102},
	}

	run := func() []string {
		q := newWSQueue(5, BackpressureDropDepthKeepOps)
		for _, in := range input {
			q.Enqueue(in.toMsg())
		}
		out := make([]string, 0, q.Len())
		for {
			msg, ok := q.Pop()
			if !ok {
				break
			}
			out = append(out, string(msg.Data))
			if q.Len() == 0 {
				break
			}
		}
		q.Close()
		return out
	}

	first := run()
	second := run()
	if !slices.Equal(first, second) {
		t.Fatalf("replay output mismatch\nfirst=%v\nsecond=%v", first, second)
	}
}

type wsMessageFixture struct {
	kind string
	n    int
}

func (f *wsMessageFixture) toMsg() *ws.WsMessage {
	switch f.kind {
	case "depth":
		return depthMsg(f.n)
	case "liquidation":
		return liquidationMsg(f.n)
	case "markprice":
		return markPriceMsg(f.n)
	default:
		return tradeMsg(f.n)
	}
}
