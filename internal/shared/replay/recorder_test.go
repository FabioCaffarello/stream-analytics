package replay

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

func newTestReader(t *testing.T, path string) *Reader {
	t.Helper()
	r, p := NewReader(path)
	if p != nil {
		t.Fatalf("NewReader: %v", p)
	}
	return r
}

func mustNextRecord(t *testing.T, r *Reader, idx int) FixtureRecord {
	t.Helper()
	rec, ok, p := r.Next()
	if p != nil {
		t.Fatalf("Next[%d]: %v", idx, p)
	}
	if !ok {
		t.Fatalf("Next[%d]: unexpected EOF", idx)
	}
	return rec
}

type spyPublisher struct {
	envelopes []envelope.Envelope
	problem   *problem.Problem
}

func (s *spyPublisher) Publish(_ context.Context, env envelope.Envelope) *problem.Problem {
	s.envelopes = append(s.envelopes, env)
	return s.problem
}

func TestRecorderPublisherPublishAppendsBeforeDelegating(t *testing.T) {
	inner := &spyPublisher{}
	path := filepath.Join(t.TempDir(), "record.jsonl")

	rp, p := NewRecorderPublisher(inner, path)
	if p != nil {
		t.Fatalf("NewRecorderPublisher: %v", p)
	}
	t.Cleanup(func() {
		_ = rp.Close()
	})

	env := envelope.Envelope{
		Type:           "marketdata.trade",
		Version:        1,
		Venue:          "binance",
		Instrument:     "BTC-USDT",
		TsExchange:     1,
		TsIngest:       2,
		Seq:            1,
		IdempotencyKey: "idem-1",
		ContentType:    envelope.ContentTypeJSON,
		Payload:        []byte(`{"price":1}`),
	}

	if p := rp.Publish(context.Background(), env); p != nil {
		t.Fatalf("Publish: %v", p)
	}
	if len(inner.envelopes) != 1 {
		t.Fatalf("inner publish count=%d want=1", len(inner.envelopes))
	}

	r := newTestReader(t, path)
	defer func() {
		_ = r.Close()
	}()
	rec := mustNextRecord(t, r, 0)
	if rec.Envelope.IdempotencyKey != env.IdempotencyKey {
		t.Fatalf("recorded idempotency key=%q want=%q", rec.Envelope.IdempotencyKey, env.IdempotencyKey)
	}
}

func TestRecorderPublisherPublishSkipsInnerOnAppendFailure(t *testing.T) {
	inner := &spyPublisher{}
	path := filepath.Join(t.TempDir(), "record.jsonl")

	rp, p := NewRecorderPublisher(inner, path)
	if p != nil {
		t.Fatalf("NewRecorderPublisher: %v", p)
	}
	if p := rp.Close(); p != nil {
		t.Fatalf("Close: %v", p)
	}

	env := envelope.Envelope{
		Type:           "marketdata.trade",
		Version:        1,
		Venue:          "binance",
		Instrument:     "BTC-USDT",
		TsExchange:     1,
		TsIngest:       2,
		Seq:            1,
		IdempotencyKey: "idem-1",
		ContentType:    envelope.ContentTypeJSON,
		Payload:        []byte(`{"price":1}`),
	}

	p = rp.Publish(context.Background(), env)
	if p == nil {
		t.Fatal("expected publish problem after writer close")
	}
	if len(inner.envelopes) != 0 {
		t.Fatalf("inner publish should not be called, got=%d", len(inner.envelopes))
	}
}
