package contracts_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/replay"
)

type mockSource struct {
	envelopes []envelope.Envelope
	closeErr  error
}

func (m *mockSource) Read(ctx context.Context) (<-chan envelope.Envelope, func() error, *problem.Problem) {
	out := make(chan envelope.Envelope, len(m.envelopes))
	for i := range m.envelopes {
		out <- m.envelopes[i]
	}
	close(out)
	return out, func() error {
		select {
		case <-ctx.Done():
		default:
		}
		return m.closeErr
	}, nil
}

func TestRecordFromSourceWritesFixture(t *testing.T) {
	mustBootstrapPayloadRegistry(t)

	envs := []envelope.Envelope{
		buildJSONFixtureEnvelope(t, 1),
		buildProtoFixtureEnvelope(t, 2),
		buildJSONFixtureEnvelope(t, 3),
	}
	src := &mockSource{envelopes: envs}
	outPath := filepath.Join(t.TempDir(), "source.jsonl")

	summary, p := replay.RecordFromSource(context.Background(), src, outPath, 0, time.Time{})
	if p != nil {
		t.Fatalf("RecordFromSource: %v", p)
	}
	if summary.ReadCount != len(envs) || summary.WrittenCount != len(envs) {
		t.Fatalf("summary counts mismatch: %+v", summary)
	}
	if summary.OutputSHA == "" {
		t.Fatal("expected non-empty output sha")
	}

	r := newTestReader(t, outPath)
	defer func() { _ = r.Close() }()
	for i := range envs {
		rec := mustNextRecord(t, r, i)
		if rec.Envelope.IdempotencyKey != envs[i].IdempotencyKey {
			t.Fatalf("idempotency mismatch[%d]: got=%q want=%q", i, rec.Envelope.IdempotencyKey, envs[i].IdempotencyKey)
		}
	}
	assertReaderEOF(t, r)
}

func TestRecordFromSourceHonorsUntilAndMaxN(t *testing.T) {
	mustBootstrapPayloadRegistry(t)

	envs := []envelope.Envelope{
		buildJSONFixtureEnvelope(t, 1),
		buildJSONFixtureEnvelope(t, 2),
		buildJSONFixtureEnvelope(t, 3),
	}
	until := time.UnixMilli(envs[1].TsIngest)
	src := &mockSource{envelopes: envs}
	outPath := filepath.Join(t.TempDir(), "source-limited.jsonl")

	summary, p := replay.RecordFromSource(context.Background(), src, outPath, 1, until)
	if p != nil {
		t.Fatalf("RecordFromSource: %v", p)
	}
	if summary.ReadCount < 1 {
		t.Fatalf("read_count=%d want>=1", summary.ReadCount)
	}
	if summary.WrittenCount != 1 {
		t.Fatalf("written_count=%d want=1", summary.WrittenCount)
	}
}

func TestRecordFromSourceCloseError(t *testing.T) {
	mustBootstrapPayloadRegistry(t)

	src := &mockSource{
		envelopes: []envelope.Envelope{buildJSONFixtureEnvelope(t, 1)},
		closeErr:  errors.New("close failed"),
	}
	_, p := replay.RecordFromSource(context.Background(), src, filepath.Join(t.TempDir(), "source-error.jsonl"), 0, time.Time{})
	if p == nil {
		t.Fatal("expected problem when source close fails")
	}
	if p.Code != problem.Unavailable {
		t.Fatalf("problem code=%s want=%s", p.Code, problem.Unavailable)
	}
}

func TestRecordFromSourceDeterministicOutput(t *testing.T) {
	mustBootstrapPayloadRegistry(t)

	envs := []envelope.Envelope{
		buildJSONFixtureEnvelope(t, 1),
		buildProtoFixtureEnvelope(t, 2),
		buildJSONFixtureEnvelope(t, 3),
	}
	srcA := &mockSource{envelopes: envs}
	srcB := &mockSource{envelopes: envs}

	outA := filepath.Join(t.TempDir(), "a.jsonl")
	outB := filepath.Join(t.TempDir(), "b.jsonl")
	if _, p := replay.RecordFromSource(context.Background(), srcA, outA, 0, time.Time{}); p != nil {
		t.Fatalf("RecordFromSource A: %v", p)
	}
	if _, p := replay.RecordFromSource(context.Background(), srcB, outB, 0, time.Time{}); p != nil {
		t.Fatalf("RecordFromSource B: %v", p)
	}
	if p := replay.CompareFixtureFiles(outA, outB); p != nil {
		t.Fatalf("deterministic compare failed: %v", p)
	}
}
