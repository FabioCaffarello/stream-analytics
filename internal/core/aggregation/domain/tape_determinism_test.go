package domain_test

import (
	"math"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
)

type tapeTrade struct {
	price float64
	size  float64
	buy   bool
	seq   int64
}

func TestTapeWindowV1_DeterministicReplay_PropertyBased(t *testing.T) {
	const runs = 1000
	rng := newDeterministicPRNG(20260304)

	for i := 0; i < runs; i++ {
		n := 1 + (rng.next() % 24)
		trades := make([]tapeTrade, 0, n)
		for j := 0; j < n; j++ {
			price := 90.0 + rng.nextUnit()*20.0
			size := 0.01 + rng.nextUnit()*4.99
			trades = append(trades, tapeTrade{price: price, size: size, buy: (rng.next() % 2) == 0, seq: int64(j + 1)})
		}

		left, _ := domain.NewTapeWindowV1("binance", "BTC-USDT", "1s", 0)
		for _, tr := range trades {
			if p := left.ApplyTrade(tr.price, tr.size, tr.buy, tr.seq); p != nil {
				t.Fatalf("left ApplyTrade: %v", p)
			}
		}
		if p := left.Close(1_000); p != nil {
			t.Fatalf("left Close: %v", p)
		}

		shuffleTrades(rng, trades)
		right, _ := domain.NewTapeWindowV1("binance", "BTC-USDT", "1s", 0)
		for _, tr := range trades {
			if p := right.ApplyTrade(tr.price, tr.size, tr.buy, tr.seq); p != nil {
				t.Fatalf("right ApplyTrade: %v", p)
			}
		}
		if p := right.Close(1_000); p != nil {
			t.Fatalf("right Close: %v", p)
		}
		assertTapeEqual(t, left, right)
	}
}

type deterministicPRNG struct {
	state int
}

func newDeterministicPRNG(seed int) *deterministicPRNG {
	if seed == 0 {
		seed = 1
	}
	return &deterministicPRNG{state: seed}
}

func (p *deterministicPRNG) next() int {
	x := p.state
	x ^= x << 13
	x ^= x >> 7
	x ^= x << 17
	// Keep it non-negative for modulo operations.
	if x < 0 {
		x = -x
		if x < 0 {
			x = 1
		}
	}
	p.state = x
	return x
}

func (p *deterministicPRNG) nextUnit() float64 {
	// Keep precision bounded and deterministic across architectures.
	return float64(p.next()&((1<<26)-1)) / float64(1<<26)
}

func shuffleTrades(p *deterministicPRNG, trades []tapeTrade) {
	for i := len(trades) - 1; i > 0; i-- {
		j := p.next() % (i + 1)
		trades[i], trades[j] = trades[j], trades[i]
	}
}

func TestTapeWindowV1_ReplicaEquivalence(t *testing.T) {
	const total = 512
	trades := make([]tapeTrade, 0, total)
	for i := 0; i < total; i++ {
		trades = append(trades, tapeTrade{
			price: 100.0 + float64((i%17)-8)*0.1,
			size:  0.1 + float64((i%9))*0.05,
			buy:   i%2 == 0,
			seq:   int64(i + 1),
		})
	}

	replica1, _ := domain.NewTapeWindowV1("binance", "BTC-USDT", "1s", 0)
	replica2, _ := domain.NewTapeWindowV1("binance", "BTC-USDT", "1s", 0)
	for _, tr := range trades {
		if p := replica1.ApplyTrade(tr.price, tr.size, tr.buy, tr.seq); p != nil {
			t.Fatalf("replica1 ApplyTrade: %v", p)
		}
	}
	for i := len(trades) - 1; i >= 0; i-- {
		tr := trades[i]
		if p := replica2.ApplyTrade(tr.price, tr.size, tr.buy, tr.seq); p != nil {
			t.Fatalf("replica2 ApplyTrade: %v", p)
		}
	}
	if p := replica1.Close(1_000); p != nil {
		t.Fatalf("replica1 Close: %v", p)
	}
	if p := replica2.Close(1_000); p != nil {
		t.Fatalf("replica2 Close: %v", p)
	}

	assertTapeEqual(t, replica1, replica2)
	if replica1.IsBurst(300) != replica2.IsBurst(300) {
		t.Fatalf("burst mismatch: %v vs %v", replica1.IsBurst(300), replica2.IsBurst(300))
	}
}

func assertTapeEqual(t *testing.T, left, right *domain.TapeWindowV1) {
	t.Helper()
	if left.TradeCount != right.TradeCount || left.BuyCount != right.BuyCount || left.SellCount != right.SellCount {
		t.Fatalf("count mismatch left=%+v right=%+v", left, right)
	}
	if left.LastSeq != right.LastSeq {
		t.Fatalf("last_seq mismatch left=%d right=%d", left.LastSeq, right.LastSeq)
	}
	for _, pair := range []struct {
		name string
		l    float64
		r    float64
	}{
		{"buy_volume", left.BuyVolume, right.BuyVolume},
		{"sell_volume", left.SellVolume, right.SellVolume},
		{"total_volume", left.TotalVolume, right.TotalVolume},
		{"buy_notional", left.BuyNotional, right.BuyNotional},
		{"sell_notional", left.SellNotional, right.SellNotional},
		{"vwap", left.VwapPrice, right.VwapPrice},
		{"rate", left.Rate(), right.Rate()},
		{"imbalance", left.Imbalance(), right.Imbalance()},
		{"max_price", left.MaxPrice, right.MaxPrice},
		{"min_price", left.MinPrice, right.MinPrice},
		{"last_price", left.LastPrice, right.LastPrice},
		{"max_trade_size", left.MaxTradeSize, right.MaxTradeSize},
	} {
		if math.Abs(pair.l-pair.r) > 1e-9 {
			t.Fatalf("%s mismatch left=%f right=%f", pair.name, pair.l, pair.r)
		}
	}
}
