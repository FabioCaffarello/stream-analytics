//go:build integration

package jetstream

import (
	"context"
	"testing"
	"time"

	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	"github.com/market-raccoon/internal/adapters/storage/clickhouse"
	"github.com/market-raccoon/internal/adapters/storage/timescale"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

func TestACKOnlyAfterCommit_Success(t *testing.T) {
	hot := &commitSpy{}
	cold := &commitSpy{}
	committer := adapterstorage.NewSnapshotCommitter(hot, cold)

	var ackAt time.Time
	p := adapterstorage.CommitAndAck(context.Background(), committer, conformanceSnapshot(1), func() error {
		ackAt = time.Now()
		return nil
	})
	if p != nil {
		t.Fatalf("CommitAndAck failed: %v", p)
	}
	if hot.count != 1 || cold.count != 1 {
		t.Fatalf("unexpected commit counts hot=%d cold=%d", hot.count, cold.count)
	}
	if ackAt.IsZero() {
		t.Fatal("ack callback not invoked")
	}
	if !ackAt.After(cold.lastAt) && !ackAt.Equal(cold.lastAt) {
		t.Fatalf("ack happened before cold commit: ack=%v cold=%v", ackAt, cold.lastAt)
	}

	disposition, status := MapProblemToDisposition(p)
	if disposition != DispositionAck || status != "ok" {
		t.Fatalf("disposition/status = %v/%s want %v/ok", disposition, status, DispositionAck)
	}
}

func TestNAKOnTransientCommitFailure(t *testing.T) {
	committer := adapterstorage.NewSnapshotCommitter(transientCommitFailSpy{}, &commitSpy{})
	acked := false

	p := adapterstorage.CommitAndAck(context.Background(), committer, conformanceSnapshot(1), func() error {
		acked = true
		return nil
	})
	if p == nil {
		t.Fatal("expected commit failure")
	}
	if acked {
		t.Fatal("ack must not run when commit fails")
	}

	disposition, status := MapProblemToDisposition(p)
	if disposition != DispositionNak || status != "nak" {
		t.Fatalf("disposition/status = %v/%s want %v/nak", disposition, status, DispositionNak)
	}
}

func TestTERMOnPoisonMessage(t *testing.T) {
	_, decodeProb := envelope.UnmarshalBinary([]byte("not-a-valid-envelope"))
	if decodeProb == nil {
		t.Fatal("expected decode failure")
	}

	decision := ClassifyIngestError(decodeProb, envelope.Envelope{})
	if decision.Disposition != DispositionTerm {
		t.Fatalf("disposition=%v want=%v", decision.Disposition, DispositionTerm)
	}
}

func TestACKTimeout_NAKOnSlowCommit(t *testing.T) {
	committer := adapterstorage.NewSnapshotCommitter(slowCommitSpy{delay: 200 * time.Millisecond}, &commitSpy{})
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	acked := false
	p := adapterstorage.CommitAndAck(ctx, committer, conformanceSnapshot(1), func() error {
		acked = true
		return nil
	})
	if p == nil {
		t.Fatal("expected timeout problem")
	}
	if acked {
		t.Fatal("ack must not run on timeout")
	}

	disposition, status := MapProblemToDisposition(p)
	if disposition != DispositionNak || status != "nak" {
		t.Fatalf("disposition/status = %v/%s want %v/nak", disposition, status, DispositionNak)
	}
}

func TestIdempotentRedelivery_ACKOnDuplicate(t *testing.T) {
	hot := timescale.NewWriter()
	cold := clickhouse.NewWriter()
	committer := adapterstorage.NewSnapshotCommitter(hot, cold)

	acked := 0
	for i := 0; i < 2; i++ {
		p := adapterstorage.CommitAndAck(context.Background(), committer, conformanceSnapshot(7), func() error {
			acked++
			return nil
		})
		if p != nil {
			t.Fatalf("commit #%d failed: %v", i+1, p)
		}
		disposition, status := MapProblemToDisposition(p)
		if disposition != DispositionAck || status != "ok" {
			t.Fatalf("commit #%d disposition/status = %v/%s want %v/ok", i+1, disposition, status, DispositionAck)
		}
	}

	if hot.CommitCount() != 1 {
		t.Fatalf("hot commits=%d want=1", hot.CommitCount())
	}
	if cold.CommitCount() != 1 {
		t.Fatalf("cold commits=%d want=1", cold.CommitCount())
	}
	if acked != 2 {
		t.Fatalf("acks=%d want=2", acked)
	}
}

type commitSpy struct {
	count  int
	lastAt time.Time
}

func (s *commitSpy) Save(context.Context, aggdomain.SnapshotProduced) *problem.Problem {
	s.count++
	s.lastAt = time.Now()
	return nil
}

type transientCommitFailSpy struct{}

func (transientCommitFailSpy) Save(context.Context, aggdomain.SnapshotProduced) *problem.Problem {
	return problem.WithRetryable(problem.New(problem.Unavailable, "temporary commit failure"))
}

type slowCommitSpy struct {
	delay time.Duration
}

func (s slowCommitSpy) Save(ctx context.Context, _ aggdomain.SnapshotProduced) *problem.Problem {
	select {
	case <-time.After(s.delay):
		return nil
	case <-ctx.Done():
		return problem.WithRetryable(problem.Wrap(ctx.Err(), problem.Unavailable, "slow commit timeout"))
	}
}

func conformanceSnapshot(seq int64) aggdomain.SnapshotProduced {
	return aggdomain.SnapshotProduced{
		BookID: aggdomain.BookID{
			Venue:      "binance",
			Instrument: "BTCUSDT",
		},
		Seq: seq,
		Bids: []aggdomain.Level{{
			Price:    100,
			Quantity: 1,
		}},
		Asks: []aggdomain.Level{{
			Price:    101,
			Quantity: 1,
		}},
	}
}
