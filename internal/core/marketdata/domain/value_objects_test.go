package domain

import "testing"

func TestNewVenueID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    VenueID
		wantErr bool
	}{
		{name: "empty string", input: "", wantErr: true},
		{name: "whitespace only", input: "   ", wantErr: true},
		{name: "tab whitespace", input: "\t\n", wantErr: true},
		{name: "valid lowercase", input: "binance", want: "BINANCE"},
		{name: "valid with surrounding whitespace", input: "  Bybit  ", want: "BYBIT"},
		{name: "already uppercase", input: "COINBASE", want: "COINBASE"},
		{name: "mixed case", input: "HyperLiquid", want: "HYPERLIQUID"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, p := NewVenueID(tc.input)
			if tc.wantErr {
				if p == nil {
					t.Fatal("expected problem, got nil")
				}
				return
			}
			if p != nil {
				t.Fatalf("unexpected problem: %v", p)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNewInstrumentID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    InstrumentID
		wantErr bool
	}{
		{name: "empty string", input: "", wantErr: true},
		{name: "whitespace only", input: "   ", wantErr: true},
		{name: "valid with dash", input: "btc-usdt", want: "BTCUSDT"},
		{name: "valid with slash", input: "eth/usd", want: "ETHUSD"},
		{name: "valid with underscore", input: "sol_usdc", want: "SOLUSDC"},
		{name: "surrounding whitespace", input: "  BTC-PERP  ", want: "BTCPERP"},
		{name: "already canonical", input: "BTCUSDT", want: "BTCUSDT"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, p := NewInstrumentID(tc.input)
			if tc.wantErr {
				if p == nil {
					t.Fatal("expected problem, got nil")
				}
				return
			}
			if p != nil {
				t.Fatalf("unexpected problem: %v", p)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNewEventType(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    EventType
		wantErr bool
	}{
		{name: "empty string", input: "", wantErr: true},
		{name: "whitespace only", input: "  \t ", wantErr: true},
		{name: "valid trade", input: "marketdata.trade", want: "marketdata.trade"},
		{name: "mixed case normalized", input: "MarketData.Trade", want: "marketdata.trade"},
		{name: "with surrounding whitespace", input: "  marketdata.bookdelta  ", want: "marketdata.bookdelta"},
		{name: "uppercase", input: "MARKETDATA.LIQUIDATION", want: "marketdata.liquidation"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, p := NewEventType(tc.input)
			if tc.wantErr {
				if p == nil {
					t.Fatal("expected problem, got nil")
				}
				return
			}
			if p != nil {
				t.Fatalf("unexpected problem: %v", p)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNewSchemaVersion(t *testing.T) {
	tests := []struct {
		name    string
		input   int
		wantErr bool
	}{
		{name: "zero", input: 0, wantErr: true},
		{name: "negative", input: -1, wantErr: true},
		{name: "negative large", input: -100, wantErr: true},
		{name: "valid minimum", input: 1},
		{name: "valid large", input: 100},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, p := NewSchemaVersion(tc.input)
			if tc.wantErr {
				if p == nil {
					t.Fatal("expected problem, got nil")
				}
				return
			}
			if p != nil {
				t.Fatalf("unexpected problem: %v", p)
			}
			if int(got) != tc.input {
				t.Fatalf("got %d, want %d", got, tc.input)
			}
		})
	}
}

func TestNewSequence(t *testing.T) {
	tests := []struct {
		name    string
		input   int64
		wantErr bool
	}{
		{name: "negative", input: -1, wantErr: true},
		{name: "negative large", input: -9999, wantErr: true},
		{name: "zero is valid", input: 0},
		{name: "positive", input: 1000},
		{name: "large value", input: 1<<32 - 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, p := NewSequence(tc.input)
			if tc.wantErr {
				if p == nil {
					t.Fatal("expected problem, got nil")
				}
				return
			}
			if p != nil {
				t.Fatalf("unexpected problem: %v", p)
			}
			if int64(got) != tc.input {
				t.Fatalf("got %d, want %d", got, tc.input)
			}
		})
	}
}

func TestNewTimestamp(t *testing.T) {
	tests := []struct {
		name    string
		input   int64
		wantErr bool
	}{
		{name: "zero", input: 0, wantErr: true},
		{name: "negative", input: -1, wantErr: true},
		{name: "minimum valid", input: 1},
		{name: "realistic millis", input: 1710000000000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, p := NewTimestamp(tc.input)
			if tc.wantErr {
				if p == nil {
					t.Fatal("expected problem, got nil")
				}
				return
			}
			if p != nil {
				t.Fatalf("unexpected problem: %v", p)
			}
			if int64(got) != tc.input {
				t.Fatalf("got %d, want %d", got, tc.input)
			}
		})
	}
}

func TestNewIdempotencyKey(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "empty", input: "", wantErr: true},
		{name: "whitespace only", input: "   ", wantErr: true},
		{name: "valid key", input: "abc123"},
		{name: "uuid-like", input: "550e8400-e29b-41d4-a716-446655440000"},
		{name: "hash-like", input: "a1b2c3d4e5f6"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, p := NewIdempotencyKey(tc.input)
			if tc.wantErr {
				if p == nil {
					t.Fatal("expected problem, got nil")
				}
				return
			}
			if p != nil {
				t.Fatalf("unexpected problem: %v", p)
			}
			if string(got) != tc.input {
				t.Fatalf("got %q, want %q", got, tc.input)
			}
		})
	}
}

func TestNewDedupWindow(t *testing.T) {
	tests := []struct {
		name    string
		input   int
		wantErr bool
	}{
		{name: "zero", input: 0, wantErr: true},
		{name: "negative", input: -1, wantErr: true},
		{name: "negative large", input: -500, wantErr: true},
		{name: "minimum valid", input: 1},
		{name: "typical value", input: 1000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, p := NewDedupWindow(tc.input)
			if tc.wantErr {
				if p == nil {
					t.Fatal("expected problem, got nil")
				}
				return
			}
			if p != nil {
				t.Fatalf("unexpected problem: %v", p)
			}
			if int(got) != tc.input {
				t.Fatalf("got %d, want %d", got, tc.input)
			}
		})
	}
}

func TestStringAccessors(t *testing.T) {
	v, _ := NewVenueID("binance")
	if v.String() != "BINANCE" {
		t.Fatalf("VenueID.String() = %q, want %q", v.String(), "BINANCE")
	}

	i, _ := NewInstrumentID("btc-usdt")
	if i.String() != "BTCUSDT" {
		t.Fatalf("InstrumentID.String() = %q, want %q", i.String(), "BTCUSDT")
	}

	e, _ := NewEventType("MarketData.Trade")
	if e.String() != "marketdata.trade" {
		t.Fatalf("EventType.String() = %q, want %q", e.String(), "marketdata.trade")
	}

	k, _ := NewIdempotencyKey("key-001")
	if k.String() != "key-001" {
		t.Fatalf("IdempotencyKey.String() = %q, want %q", k.String(), "key-001")
	}
}

func TestInt64Accessor(t *testing.T) {
	seq, _ := NewSequence(42)
	if seq.Int64() != 42 {
		t.Fatalf("Sequence.Int64() = %d, want 42", seq.Int64())
	}
}

func TestUnixMilliAccessor(t *testing.T) {
	ts, _ := NewTimestamp(1710000000000)
	if ts.UnixMilli() != 1710000000000 {
		t.Fatalf("Timestamp.UnixMilli() = %d, want 1710000000000", ts.UnixMilli())
	}
}

func TestDedupWindowSizeAccessor(t *testing.T) {
	w, _ := NewDedupWindow(256)
	if w.Size() != 256 {
		t.Fatalf("DedupWindow.Size() = %d, want 256", w.Size())
	}
}
