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

func TestIsInstrumentSymbolEquivalent(t *testing.T) {
	if !domain.IsInstrumentSymbolEquivalent("BTC-USDT", "BTCUSDT") {
		t.Fatal("expected BTC-USDT and BTCUSDT to be equivalent")
	}
	if domain.IsInstrumentSymbolEquivalent("BTCUSDT", "ETHUSDT") {
		t.Fatal("expected BTCUSDT and ETHUSDT not equivalent")
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
