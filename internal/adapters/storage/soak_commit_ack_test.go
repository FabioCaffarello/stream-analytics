package storage_test

import (
	"context"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/adapters/storage"
	"github.com/FabioCaffarello/stream-analytics/internal/adapters/storage/clickhouse"
	"github.com/FabioCaffarello/stream-analytics/internal/adapters/storage/timescale"
	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
)

func TestStorageSoak_Burst10x60s_CommitAckInvariants(t *testing.T) {
	hot := timescale.NewWriter()
	cold := clickhouse.NewWriter()
	committer := storage.NewSnapshotCommitter(hot, cold)

	const (
		seconds         = 60
		burstMultiplier = 10
		total           = seconds * burstMultiplier
	)

	ackedSeqs := make([]int64, 0, total)
	lastAckSeq := int64(0)

	for sec := 0; sec < seconds; sec++ {
		for burst := 0; burst < burstMultiplier; burst++ {
			seq := int64(sec*burstMultiplier + burst + 1)
			snap := soakSnapshot(seq)

			p := storage.CommitAndAck(context.Background(), committer, snap, func() error {
				if hot.CommitCount() < int(seq) || cold.CommitCount() < int(seq) {
					t.Fatalf("ack before durable write seq=%d hot=%d cold=%d", seq, hot.CommitCount(), cold.CommitCount())
				}
				if seq <= lastAckSeq {
					t.Fatalf("ack seq not monotonic seq=%d last=%d", seq, lastAckSeq)
				}
				lastAckSeq = seq
				ackedSeqs = append(ackedSeqs, seq)
				return nil
			})
			if p != nil {
				t.Fatalf("commit+ack failed seq=%d: %v", seq, p)
			}
		}
	}

	if got := hot.CommitCount(); got != total {
		t.Fatalf("hot commit count=%d want=%d", got, total)
	}
	if got := cold.CommitCount(); got != total {
		t.Fatalf("cold commit count=%d want=%d", got, total)
	}
	if got := len(ackedSeqs); got != total {
		t.Fatalf("acked count=%d want=%d", got, total)
	}

	seen := make(map[int64]struct{}, total)
	for i, seq := range ackedSeqs {
		want := int64(i + 1)
		if seq != want {
			t.Fatalf("zero-gap violated at idx=%d got=%d want=%d", i, seq, want)
		}
		if _, dup := seen[seq]; dup {
			t.Fatalf("zero-dup violated at seq=%d", seq)
		}
		seen[seq] = struct{}{}
	}
}

func soakSnapshot(seq int64) aggdomain.SnapshotProduced {
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
