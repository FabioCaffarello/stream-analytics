package domain_test

import (
	"testing"

	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/shared/envelope"
)

func TestParseSubject(t *testing.T) {
	sub, p := domain.ParseSubject("marketdata.trade/binance/BTC-USDT/1m")
	if p != nil {
		t.Fatalf("ParseSubject: %v", p)
	}
	if got, want := sub.String(), "marketdata.trade/binance/BTCUSDT/1m"; got != want {
		t.Fatalf("subject = %q, want %q", got, want)
	}
}

func TestParseSubject_Signal(t *testing.T) {
	sub, p := domain.ParseSubject("signal/absorption/binance/BTC-USDT/1m")
	if p != nil {
		t.Fatalf("ParseSubject signal: %v", p)
	}
	if got, want := sub.String(), "signal/absorption/binance/BTCUSDT/1m"; got != want {
		t.Fatalf("subject = %q, want %q", got, want)
	}
	if !sub.IsSignal() {
		t.Fatal("expected signal subject")
	}
}

func TestParseSubject_SignalWildcardKind(t *testing.T) {
	sub, p := domain.ParseSubject("signal/*/binance/BTC-USDT/1m")
	if p != nil {
		t.Fatalf("ParseSubject signal wildcard: %v", p)
	}
	if got, want := sub.String(), "signal/*/binance/BTCUSDT/1m"; got != want {
		t.Fatalf("subject = %q, want %q", got, want)
	}
	if !sub.IsSignal() {
		t.Fatal("expected signal subject")
	}
}

func TestParseSubject_invalid(t *testing.T) {
	_, p := domain.ParseSubject("marketdata.trade/binance/BTC-USDT")
	if p == nil {
		t.Fatal("expected error")
	}
}

func TestSubjectFromEnvelope(t *testing.T) {
	env := envelope.Envelope{Type: "marketdata.bookdelta", Venue: "binance", Instrument: "BTC-USDT", ContentType: ""}
	sub, p := domain.SubjectFromEnvelope(env, "raw")
	if p != nil {
		t.Fatalf("SubjectFromEnvelope: %v", p)
	}
	if got, want := sub.String(), "marketdata.bookdelta/binance/BTCUSDT/raw"; got != want {
		t.Fatalf("subject = %q, want %q", got, want)
	}
}

func TestSubjectFromEnvelope_Signal(t *testing.T) {
	env := envelope.Envelope{
		Type:       "signal.composite",
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Meta: map[string]string{
			"kind": "absorption",
		},
	}
	sub, p := domain.SubjectFromEnvelope(env, "1m")
	if p != nil {
		t.Fatalf("SubjectFromEnvelope signal: %v", p)
	}
	if got, want := sub.String(), "signal/absorption/binance/BTCUSDT/1m"; got != want {
		t.Fatalf("subject = %q, want %q", got, want)
	}
}

func TestSubjectFromEnvelope_SignalEvent(t *testing.T) {
	env := envelope.Envelope{
		Type:       "signal.event",
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Meta: map[string]string{
			"kind": "liquidity_collapse",
		},
	}
	sub, p := domain.SubjectFromEnvelope(env, "raw")
	if p != nil {
		t.Fatalf("SubjectFromEnvelope signal.event: %v", p)
	}
	if got, want := sub.String(), "signal/liquidity_collapse/binance/BTCUSDT/raw"; got != want {
		t.Fatalf("subject = %q, want %q", got, want)
	}
}

func TestIsInstrumentSymbolEquivalent(t *testing.T) {
	if !domain.IsInstrumentSymbolEquivalent("BTC-USDT", "BTCUSDT") {
		t.Fatal("expected BTC-USDT and BTCUSDT to be equivalent")
	}
	if domain.IsInstrumentSymbolEquivalent("BTCUSDT", "ETHUSDT") {
		t.Fatal("expected BTCUSDT and ETHUSDT not equivalent")
	}
}

func TestTimeframeFromEnvelopeMeta(t *testing.T) {
	tests := []struct {
		name     string
		meta     map[string]string
		fallback string
		want     string
	}{
		{"meta present", map[string]string{"timeframe": "5m"}, "raw", "5m"},
		{"meta empty string", map[string]string{"timeframe": ""}, "raw", "raw"},
		{"meta nil", nil, "raw", "raw"},
		{"meta no timeframe key", map[string]string{"other": "val"}, "raw", "raw"},
		{"meta whitespace", map[string]string{"timeframe": "  1h  "}, "raw", "1h"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := envelope.Envelope{Meta: tt.meta}
			got := domain.TimeframeFromEnvelopeMeta(env, tt.fallback)
			if got != tt.want {
				t.Fatalf("TimeframeFromEnvelopeMeta() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSnapshotSubject(t *testing.T) {
	sub, p := domain.SnapshotSubject("binance", "BTC-USDT", "raw")
	if p != nil {
		t.Fatalf("SnapshotSubject: %v", p)
	}
	if got, want := sub.String(), "aggregation.snapshot/binance/BTCUSDT/raw"; got != want {
		t.Fatalf("subject=%q want=%q", got, want)
	}
}
