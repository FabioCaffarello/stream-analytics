package storage_test

import (
	"context"
	"sync"
	"testing"
	"time"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

type commitOrderSpy struct {
	mu         sync.Mutex
	commits    []string
	commitTime time.Time
}

func (s *commitOrderSpy) Save(_ context.Context, snap aggdomain.SnapshotProduced) *problem.Problem {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commits = append(s.commits, snap.BookID.Venue+"/"+snap.BookID.Instrument)
	s.commitTime = time.Now()
	return nil
}

func (s *commitOrderSpy) CommitCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.commits)
}

func (s *commitOrderSpy) LastCommitTime() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.commitTime
}

type transientFailSpy struct{}

func (transientFailSpy) Save(context.Context, aggdomain.SnapshotProduced) *problem.Problem {
	return problem.WithRetryable(problem.New(problem.Unavailable, "transient commit failure"))
}

type slowCommitSpy struct {
	delay time.Duration
}

func (s slowCommitSpy) Save(ctx context.Context, _ aggdomain.SnapshotProduced) *problem.Problem {
	select {
	case <-time.After(s.delay):
		return nil
	case <-ctx.Done():
		return problem.WithRetryable(problem.Wrap(ctx.Err(), problem.Unavailable, "commit canceled"))
	}
}

func TestCommitOrderSpyRecordsCommit(t *testing.T) {
	spy := &commitOrderSpy{}
	if p := spy.Save(context.Background(), testSnapshot()); p != nil {
		t.Fatalf("save failed: %v", p)
	}
	if got := spy.CommitCount(); got != 1 {
		t.Fatalf("commit count=%d want=1", got)
	}
	if spy.LastCommitTime().IsZero() {
		t.Fatal("expected non-zero commit time")
	}
}

func TestTransientFailSpyReturnsRetryableProblem(t *testing.T) {
	p := transientFailSpy{}.Save(context.Background(), testSnapshot())
	if p == nil {
		t.Fatal("expected transient failure problem")
	}
	if !p.Retryable {
		t.Fatal("expected retryable problem")
	}
}

func TestSlowCommitSpyHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := (slowCommitSpy{delay: 50 * time.Millisecond}).Save(ctx, testSnapshot())
	if p == nil {
		t.Fatal("expected canceled commit problem")
	}
	if !p.Retryable {
		t.Fatal("expected retryable canceled problem")
	}
}
