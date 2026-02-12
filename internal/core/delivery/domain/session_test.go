package domain_test

import (
	"testing"

	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

func mustSubject(t *testing.T, raw string) domain.Subject {
	t.Helper()
	s, p := domain.ParseSubject(raw)
	if p != nil {
		t.Fatalf("ParseSubject(%q): %v", raw, p)
	}
	return s
}

func TestNewSession(t *testing.T) {
	s := domain.NewSession()
	if s.ID() == "" {
		t.Error("session ID must not be empty")
	}
	if len(s.Subscriptions()) != 0 {
		t.Error("new session should have no subscriptions")
	}
}

func TestSession_subscribe(t *testing.T) {
	s := domain.NewSession()
	sub := mustSubject(t, "marketdata.trade/binance/BTC-USDT/raw")
	p := s.Subscribe(sub, domain.Filter{})
	if p != nil {
		t.Fatalf("Subscribe: %s", p)
	}
	if !s.IsSubscribed(sub) {
		t.Error("should be subscribed")
	}
	if len(s.Subscriptions()) != 1 {
		t.Errorf("expected 1 subscription, got %d", len(s.Subscriptions()))
	}
}

func TestSession_unsubscribe(t *testing.T) {
	s := domain.NewSession()
	sub := mustSubject(t, "marketdata.trade/binance/BTC-USDT/raw")
	_ = s.Subscribe(sub, domain.Filter{})
	p := s.Unsubscribe(sub)
	if p != nil {
		t.Fatalf("Unsubscribe: %s", p)
	}
	if s.IsSubscribed(sub) {
		t.Error("should not be subscribed after unsubscribe")
	}
}

func TestSession_unsubscribe_notFound(t *testing.T) {
	s := domain.NewSession()
	sub := mustSubject(t, "marketdata.trade/binance/BTC-USDT/raw")
	p := s.Unsubscribe(sub)
	if p == nil {
		t.Fatal("expected NotFound problem")
	}
	if p.Code != problem.NotFound {
		t.Errorf("code = %s; want NOT_FOUND", p.Code)
	}
}

func TestSession_uniqueSubjects(t *testing.T) {
	s := domain.NewSession()
	sub := mustSubject(t, "marketdata.trade/binance/BTC-USDT/raw")
	_ = s.Subscribe(sub, domain.Filter{})
	_ = s.Subscribe(sub, domain.Filter{MinSpread: 10}) // update same subject
	if len(s.Subscriptions()) != 1 {
		t.Errorf("duplicate subscription; expected 1, got %d", len(s.Subscriptions()))
	}
}
