package timescale_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/market-raccoon/internal/adapters/storage/timescale"
	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
	portfolioapp "github.com/market-raccoon/internal/core/portfolio/app"
	domain "github.com/market-raccoon/internal/core/portfolio/domain"
)

// TestReplayDeterminism verifies that replaying the same sequence of execution
// events produces identical portfolio states, snapshots, and summaries.
func TestReplayDeterminism(t *testing.T) {
	events := testExecutionEventSequence()

	// Run projector twice with same event sequence.
	run := func() ([]domain.PortfolioStateV1, domain.AccountSnapshotV1, domain.PortfolioSummaryV1) {
		proj := portfolioapp.NewBootstrapProjector(portfolioapp.DefaultProjectorConfig())
		var states []domain.PortfolioStateV1
		for _, evt := range events {
			if s, ok := proj.Apply(evt); ok {
				states = append(states, s)
			}
		}
		snap, _ := proj.BuildAccountSnapshot("acct-1", 1_710_000_060_000)
		sum, _ := proj.BuildPortfolioSummary(1_710_000_060_000)
		return states, snap, sum
	}

	states1, snap1, sum1 := run()
	states2, snap2, sum2 := run()

	if len(states1) != len(states2) {
		t.Fatalf("state count mismatch: %d vs %d", len(states1), len(states2))
	}
	for i := range states1 {
		if states1[i].StateID != states2[i].StateID {
			t.Fatalf("state[%d] id mismatch: %q vs %q", i, states1[i].StateID, states2[i].StateID)
		}
		if states1[i].EquityUSD != states2[i].EquityUSD {
			t.Fatalf("state[%d] equity mismatch: %f vs %f", i, states1[i].EquityUSD, states2[i].EquityUSD)
		}
		if states1[i].RealizedPnlUSD != states2[i].RealizedPnlUSD {
			t.Fatalf("state[%d] realized mismatch", i)
		}
	}

	if snap1.SnapshotID != snap2.SnapshotID {
		t.Fatalf("snapshot id mismatch: %q vs %q", snap1.SnapshotID, snap2.SnapshotID)
	}
	if snap1.TotalEquityUSD != snap2.TotalEquityUSD {
		t.Fatalf("snapshot equity mismatch")
	}

	if sum1.SummaryID != sum2.SummaryID {
		t.Fatalf("summary id mismatch: %q vs %q", sum1.SummaryID, sum2.SummaryID)
	}
	if sum1.GlobalEquityUSD != sum2.GlobalEquityUSD {
		t.Fatalf("summary equity mismatch")
	}
}

// TestReplayIdempotentUpsert verifies that upserting the same projected state
// twice is idempotent (no error, no duplicate).
func TestReplayIdempotentUpsert(t *testing.T) {
	exec := &fakeSQLExecutor{rows: 1}

	stateW := timescale.NewPgPortfolioStateWriterWithExecutor(exec)
	snapW := timescale.NewPgAccountSnapshotWriterWithExecutor(exec)
	sumW := timescale.NewPgPortfolioSummaryWriterWithExecutor(exec)

	state := testPortfolioState()
	snap := testAccountSnapshot()
	sum := testPortfolioSummary()

	// First upsert
	if p := stateW.UpsertPortfolioState(context.Background(), state); p != nil {
		t.Fatalf("first state upsert: %v", p)
	}
	if p := snapW.UpsertAccountSnapshot(context.Background(), snap); p != nil {
		t.Fatalf("first snap upsert: %v", p)
	}
	if p := sumW.UpsertPortfolioSummary(context.Background(), sum); p != nil {
		t.Fatalf("first summary upsert: %v", p)
	}

	// Second upsert (idempotent)
	if p := stateW.UpsertPortfolioState(context.Background(), state); p != nil {
		t.Fatalf("second state upsert: %v", p)
	}
	if p := snapW.UpsertAccountSnapshot(context.Background(), snap); p != nil {
		t.Fatalf("second snap upsert: %v", p)
	}
	if p := sumW.UpsertPortfolioSummary(context.Background(), sum); p != nil {
		t.Fatalf("second summary upsert: %v", p)
	}

	// Verify ON CONFLICT clauses
	if !strings.Contains(exec.lastQuery, "ON CONFLICT") {
		t.Fatalf("expected ON CONFLICT in query: %s", exec.lastQuery)
	}
}

// TestStateSerializationRoundTrip verifies JSON marshaling of nested fields
// matches what the writer sends and reader expects.
func TestStateSerializationRoundTrip(t *testing.T) {
	state := testPortfolioState()

	// Marshal like the writer does
	balancesJSON, _ := json.Marshal(state.Balances)
	positionsJSON, _ := json.Marshal(state.Positions)
	exposuresJSON, _ := json.Marshal(state.Exposures)
	riskJSON, _ := json.Marshal(state.Risk)
	fillJSON, _ := json.Marshal(state.FillSummary)
	provJSON, _ := json.Marshal(state.Provenance)

	// Unmarshal like the reader does
	var balances []domain.BalanceV1
	var positions []domain.PositionV1
	var exposures []domain.ExposureV1
	var risk domain.RiskSnapshotV1
	var fill domain.FillSummaryV1
	var prov domain.ProjectionProvenanceV1

	if err := json.Unmarshal(balancesJSON, &balances); err != nil {
		t.Fatalf("balances roundtrip: %v", err)
	}
	if err := json.Unmarshal(positionsJSON, &positions); err != nil {
		t.Fatalf("positions roundtrip: %v", err)
	}
	if err := json.Unmarshal(exposuresJSON, &exposures); err != nil {
		t.Fatalf("exposures roundtrip: %v", err)
	}
	if err := json.Unmarshal(riskJSON, &risk); err != nil {
		t.Fatalf("risk roundtrip: %v", err)
	}
	if err := json.Unmarshal(fillJSON, &fill); err != nil {
		t.Fatalf("fill_summary roundtrip: %v", err)
	}
	if err := json.Unmarshal(provJSON, &prov); err != nil {
		t.Fatalf("provenance roundtrip: %v", err)
	}

	if len(balances) != len(state.Balances) {
		t.Fatalf("balances count: %d vs %d", len(balances), len(state.Balances))
	}
	if balances[0].Asset != state.Balances[0].Asset {
		t.Fatalf("balance asset: %q vs %q", balances[0].Asset, state.Balances[0].Asset)
	}
	if positions[0].Symbol != state.Positions[0].Symbol {
		t.Fatalf("position symbol: %q vs %q", positions[0].Symbol, state.Positions[0].Symbol)
	}
	if risk.MarginUsedUSD != state.Risk.MarginUsedUSD {
		t.Fatalf("risk margin: %f vs %f", risk.MarginUsedUSD, state.Risk.MarginUsedUSD)
	}
	if fill.TotalTradeCount != state.FillSummary.TotalTradeCount {
		t.Fatalf("fill trade count: %d vs %d", fill.TotalTradeCount, state.FillSummary.TotalTradeCount)
	}
	if prov.SourceExecutionEventID != state.Provenance.SourceExecutionEventID {
		t.Fatalf("provenance event id: %q vs %q", prov.SourceExecutionEventID, state.Provenance.SourceExecutionEventID)
	}
}

func testExecutionEventSequence() []executiondomain.ExecutionEventV1 {
	return []executiondomain.ExecutionEventV1{
		{
			EventID:      "evt-001",
			ExecutionSeq: 1,
			Attempt:      1,
			Status:       executiondomain.ExecutionStatusPlaced,
			Correlation: executiondomain.ExecutionCorrelation{
				IntentID:  "intent-001",
				AccountID: "acct-1",
				Venue:     "binance",
				Symbol:    "BTCUSDT",
				OrderID:   "ord-001",
			},
			RequestedQty: 0.5,
			LimitPrice:   60000,
			TsEventMs:    1_710_000_000_000,
			Provenance: executiondomain.ExecutionProvenance{
				CorrelationID: "corr-001",
				TraceID:       "trace-001",
				Source:        "test",
			},
		},
		{
			EventID:      "evt-002",
			ExecutionSeq: 2,
			Attempt:      1,
			Status:       executiondomain.ExecutionStatusFilled,
			Correlation: executiondomain.ExecutionCorrelation{
				IntentID:  "intent-001",
				AccountID: "acct-1",
				Venue:     "binance",
				Symbol:    "BTCUSDT",
				OrderID:   "ord-001",
			},
			RequestedQty:        0.5,
			LastFillQty:         0.5,
			LastFillPrice:       60000,
			CumulativeFilledQty: 0.5,
			TsEventMs:           1_710_000_030_000,
			Provenance: executiondomain.ExecutionProvenance{
				CorrelationID: "corr-001",
				TraceID:       "trace-001",
				Source:        "test",
			},
		},
		{
			EventID:      "evt-003",
			ExecutionSeq: 3,
			Attempt:      1,
			Status:       executiondomain.ExecutionStatusPlaced,
			Correlation: executiondomain.ExecutionCorrelation{
				IntentID:  "intent-002",
				AccountID: "acct-1",
				Venue:     "binance",
				Symbol:    "BTCUSDT",
				OrderID:   "ord-002",
			},
			RequestedQty: -0.5,
			LimitPrice:   61000,
			TsEventMs:    1_710_000_050_000,
			Provenance: executiondomain.ExecutionProvenance{
				CorrelationID: "corr-002",
				TraceID:       "trace-002",
				Source:        "test",
			},
		},
		{
			EventID:      "evt-004",
			ExecutionSeq: 4,
			Attempt:      1,
			Status:       executiondomain.ExecutionStatusFilled,
			Correlation: executiondomain.ExecutionCorrelation{
				IntentID:  "intent-002",
				AccountID: "acct-1",
				Venue:     "binance",
				Symbol:    "BTCUSDT",
				OrderID:   "ord-002",
			},
			RequestedQty:        -0.5,
			LastFillQty:         0.5,
			LastFillPrice:       61000,
			CumulativeFilledQty: 0.5,
			TsEventMs:           1_710_000_060_000,
			Provenance: executiondomain.ExecutionProvenance{
				CorrelationID: "corr-002",
				TraceID:       "trace-002",
				Source:        "test",
			},
		},
	}
}
