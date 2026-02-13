package storage_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/market-raccoon/internal/adapters/storage"
	"github.com/market-raccoon/internal/adapters/storage/clickhouse"
	"github.com/market-raccoon/internal/adapters/storage/timescale"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/core/aggregation/ports"
	"github.com/market-raccoon/internal/shared/problem"
)

type blockingHotWriter struct {
	start   chan struct{}
	release chan struct{}
}

func (w *blockingHotWriter) Save(context.Context, aggdomain.SnapshotProduced) *problem.Problem {
	close(w.start)
	<-w.release
	return nil
}

type noopColdWriter struct{}

func (noopColdWriter) Save(context.Context, aggdomain.SnapshotProduced) *problem.Problem { return nil }

type failColdWriter struct{}

func (failColdWriter) Save(context.Context, aggdomain.SnapshotProduced) *problem.Problem {
	return problem.New(problem.Unavailable, "cold write failed")
}

var _ ports.HotReadModelStore = (*blockingHotWriter)(nil)
var _ ports.ColdReadModelStore = (*noopColdWriter)(nil)

func TestStorageAckOnCommit_NotOnEnqueue(t *testing.T) {
	hot := &blockingHotWriter{start: make(chan struct{}), release: make(chan struct{})}
	committer := storage.NewSnapshotCommitter(hot, noopColdWriter{})

	var (
		mu      sync.Mutex
		acked   bool
		ackTime time.Time
	)

	done := make(chan *problem.Problem, 1)
	go func() {
		done <- storage.CommitAndAck(context.Background(), committer, testSnapshot(), func() error {
			mu.Lock()
			defer mu.Unlock()
			acked = true
			ackTime = time.Now()
			return nil
		})
	}()

	<-hot.start
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if acked {
		t.Fatal("ack happened before storage commit")
	}
	mu.Unlock()

	hot.release <- struct{}{}

	if p := <-done; p != nil {
		t.Fatalf("CommitAndAck failed: %v", p)
	}

	mu.Lock()
	defer mu.Unlock()
	if !acked {
		t.Fatal("ack did not happen after commit")
	}
	if ackTime.IsZero() {
		t.Fatal("ack timestamp not recorded")
	}
}

func TestStorageAckOnCommit_NoAckWhenCommitFails(t *testing.T) {
	hot := timescale.NewWriter()
	committer := storage.NewSnapshotCommitter(hot, failColdWriter{})
	acked := false

	p := storage.CommitAndAck(context.Background(), committer, testSnapshot(), func() error {
		acked = true
		return nil
	})
	if p == nil {
		t.Fatal("expected commit failure")
	}
	if acked {
		t.Fatal("ack must not be called when commit fails")
	}
}

func TestStorageIdempotency_DuplicateSnapshotCommitsOncePerPath(t *testing.T) {
	hot := timescale.NewWriter()
	cold := clickhouse.NewWriter()
	committer := storage.NewSnapshotCommitter(hot, cold)

	snap := testSnapshot()
	if p := committer.Commit(context.Background(), snap); p != nil {
		t.Fatalf("first commit failed: %v", p)
	}
	if p := committer.Commit(context.Background(), snap); p != nil {
		t.Fatalf("second commit failed: %v", p)
	}

	if got := hot.CommitCount(); got != 1 {
		t.Fatalf("hot commits=%d want=1", got)
	}
	if got := cold.CommitCount(); got != 1 {
		t.Fatalf("cold commits=%d want=1", got)
	}
}

func testSnapshot() aggdomain.SnapshotProduced {
	return aggdomain.SnapshotProduced{
		BookID: aggdomain.BookID{Venue: "binance", Instrument: "BTCUSDT"},
		Seq:    42,
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
