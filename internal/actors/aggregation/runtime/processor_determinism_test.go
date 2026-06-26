package aggruntime

import (
	"slices"
	"testing"

	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	insightsapp "github.com/FabioCaffarello/stream-analytics/internal/core/insights/app"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/policykit"
)

func TestSortedOrderBookKeys_DeterministicOrder(t *testing.T) {
	active := map[aggdomain.BookID]struct{}{
		{Venue: "BINANCE", Instrument: "BTC-USDT"}: {},
		{Venue: "BINANCE", Instrument: "ETH-USDT"}: {},
		{Venue: "BYBIT", Instrument: "BTC-USDT"}:   {},
	}
	keys := sortedOrderBookKeys(active)
	got := make([]string, 0, len(keys))
	for _, key := range keys {
		got = append(got, key.Venue+"|"+key.Instrument)
	}
	want := []string{
		"BINANCE|BTC-USDT",
		"BINANCE|ETH-USDT",
		"BYBIT|BTC-USDT",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("sorted orderbook keys mismatch: got=%v want=%v", got, want)
	}
}

func TestSortedHeatmapKeys_DeterministicOrder(t *testing.T) {
	active := map[insightsapp.HeatmapSnapshotKey]struct{}{
		{Venue: "BINANCE", Instrument: "BTC-USDT", Timeframe: "5m"}: {},
		{Venue: "BINANCE", Instrument: "BTC-USDT", Timeframe: "1m"}: {},
		{Venue: "BINANCE", Instrument: "ETH-USDT", Timeframe: "1m"}: {},
	}
	keys := sortedHeatmapKeys(active)
	got := make([]string, 0, len(keys))
	for _, key := range keys {
		got = append(got, key.Venue+"|"+key.Instrument+"|"+key.Timeframe)
	}
	want := []string{
		"BINANCE|BTC-USDT|1m",
		"BINANCE|BTC-USDT|5m",
		"BINANCE|ETH-USDT|1m",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("sorted heatmap keys mismatch: got=%v want=%v", got, want)
	}
}

func TestSortedVolumeKeys_DeterministicOrder(t *testing.T) {
	active := map[insightsapp.VolumeProfileSnapshotKey]struct{}{
		{Venue: "BYBIT", Instrument: "BTC-USDT", Timeframe: "1m"}:   {},
		{Venue: "BINANCE", Instrument: "BTC-USDT", Timeframe: "1m"}: {},
		{Venue: "BINANCE", Instrument: "BTC-USDT", Timeframe: "5m"}: {},
	}
	keys := sortedVolumeKeys(active)
	got := make([]string, 0, len(keys))
	for _, key := range keys {
		got = append(got, key.Venue+"|"+key.Instrument+"|"+key.Timeframe)
	}
	want := []string{
		"BINANCE|BTC-USDT|1m",
		"BINANCE|BTC-USDT|5m",
		"BYBIT|BTC-USDT|1m",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("sorted volume keys mismatch: got=%v want=%v", got, want)
	}
}

func TestCollectCrossVenueBooks_DeterministicOrder(t *testing.T) {
	p := &ProcessorSubsystemActor{
		crossVenueBooks: map[string]map[string]aggdomain.CrossVenueVenueBook{
			"BTC-USDT": {
				"BYBIT":   {Venue: "BYBIT"},
				"BINANCE": {Venue: "BINANCE"},
				"KRAKEN":  {Venue: "KRAKEN"},
			},
		},
	}
	books := p.collectCrossVenueBooks("BTC-USDT")
	got := make([]string, 0, len(books))
	for _, book := range books {
		got = append(got, book.Venue)
	}
	want := []string{"BINANCE", "BYBIT", "KRAKEN"}
	if !slices.Equal(got, want) {
		t.Fatalf("cross-venue books order mismatch: got=%v want=%v", got, want)
	}
}

func TestInternPolicyPartitionWithCap_EvictsOldestAndBoundsState(t *testing.T) {
	p := &ProcessorSubsystemActor{
		policyLevels:     make(map[string]policykit.Level),
		policyPartitions: make(map[string]string),
	}
	keys := []string{
		"marketdata.trade|BINANCE|BTCUSDT",
		"marketdata.trade|BINANCE|ETHUSDT",
		"marketdata.trade|BYBIT|BTCUSDT",
		"marketdata.trade|BYBIT|ETHUSDT",
	}
	for _, key := range keys[:3] {
		partition := p.internPolicyPartitionWithCap(key, 3)
		p.policyLevels[partition] = policykit.L1
	}
	if got := len(p.policyPartitions); got != 3 {
		t.Fatalf("partition cache len=%d want=3", got)
	}
	if got := len(p.policyLevels); got != 3 {
		t.Fatalf("level cache len=%d want=3", got)
	}

	partition := p.internPolicyPartitionWithCap(keys[3], 3)
	p.policyLevels[partition] = policykit.L2

	if got := len(p.policyPartitions); got != 3 {
		t.Fatalf("partition cache len=%d want=3 after eviction", got)
	}
	if got := len(p.policyLevels); got != 3 {
		t.Fatalf("level cache len=%d want=3 after eviction", got)
	}
	if _, ok := p.policyPartitions[keys[0]]; ok {
		t.Fatalf("oldest partition key %q should be evicted", keys[0])
	}
	if _, ok := p.policyLevels[keys[0]]; ok {
		t.Fatalf("oldest level key %q should be evicted", keys[0])
	}
	for _, key := range keys[1:] {
		if _, ok := p.policyPartitions[key]; !ok {
			t.Fatalf("partition key %q missing after eviction", key)
		}
		if _, ok := p.policyLevels[key]; !ok {
			t.Fatalf("level key %q missing after eviction", key)
		}
	}
}

func TestInternPolicyPartitionWithCap_ReusesExistingKeyWithoutEviction(t *testing.T) {
	p := &ProcessorSubsystemActor{
		policyLevels:     make(map[string]policykit.Level),
		policyPartitions: make(map[string]string),
	}
	key := "marketdata.trade|BINANCE|BTCUSDT"
	first := p.internPolicyPartitionWithCap(key, 2)
	second := p.internPolicyPartitionWithCap(key, 2)
	if first != second {
		t.Fatalf("partition mismatch first=%q second=%q", first, second)
	}
	if got := len(p.policyPartitions); got != 1 {
		t.Fatalf("partition cache len=%d want=1", got)
	}
	if got := len(p.policyPartitionQ); got != 1 {
		t.Fatalf("partition queue len=%d want=1", got)
	}
	if p.policyPartitionI != 0 {
		t.Fatalf("partition queue index=%d want=0", p.policyPartitionI)
	}
}

func TestInternPolicyPartitionWithCap_EmptyQueueEvictsDeterministically(t *testing.T) {
	p := &ProcessorSubsystemActor{
		policyLevels: map[string]policykit.Level{
			"marketdata.trade|BINANCE|BTCUSDT": policykit.L1,
			"marketdata.trade|BINANCE|ETHUSDT": policykit.L1,
			"marketdata.trade|BYBIT|BTCUSDT":   policykit.L1,
		},
		policyPartitions: map[string]string{
			"marketdata.trade|BINANCE|BTCUSDT": "marketdata.trade|BINANCE|BTCUSDT",
			"marketdata.trade|BINANCE|ETHUSDT": "marketdata.trade|BINANCE|ETHUSDT",
			"marketdata.trade|BYBIT|BTCUSDT":   "marketdata.trade|BYBIT|BTCUSDT",
		},
		// Simulate a recovered actor state where the map is populated but queue state is absent.
		policyPartitionQ: nil,
	}

	newKey := "marketdata.trade|COINBASE|BTCUSD"
	partition := p.internPolicyPartitionWithCap(newKey, 3)
	p.policyLevels[partition] = policykit.L2

	if got := len(p.policyPartitions); got != 3 {
		t.Fatalf("partition cache len=%d want=3", got)
	}
	if got := len(p.policyLevels); got != 3 {
		t.Fatalf("level cache len=%d want=3", got)
	}
	// Deterministic fallback evicts lexical minimum key when queue is empty.
	evicted := "marketdata.trade|BINANCE|BTCUSDT"
	if _, ok := p.policyPartitions[evicted]; ok {
		t.Fatalf("expected deterministic eviction of %q", evicted)
	}
	if _, ok := p.policyLevels[evicted]; ok {
		t.Fatalf("expected deterministic level eviction of %q", evicted)
	}
	if _, ok := p.policyPartitions[newKey]; !ok {
		t.Fatalf("expected new key %q to be interned", newKey)
	}
}
