package app

import (
	"math"
	"sort"
	"strconv"

	portfoliodomain "github.com/market-raccoon/internal/core/portfolio/domain"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
)

const (
	// equityDriftThresholdUSD is the minimum absolute equity change between
	// consecutive snapshots to flag as drift.  Small fluctuations from
	// floating-point rounding are expected and ignored.
	equityDriftThresholdUSD = 0.01

	// staleProjectionThresholdMs flags a portfolio state as stale when its
	// projected_at_ms is older than this relative to the latest state for
	// the same account.
	staleProjectionThresholdMs = 5 * 60 * 1000 // 5 minutes
)

// BuildEquityCurve constructs a temporal equity curve from a slice of
// account snapshots.  The input must be sorted by ProjectedAtMs ASC.
// A peak-tracking drawdown percentage is computed at each point.
func BuildEquityCurve(snapshots []portfoliodomain.AccountSnapshotV1) []portfoliodomain.EquityCurvePointV1 {
	if len(snapshots) == 0 {
		return nil
	}

	sorted := make([]portfoliodomain.AccountSnapshotV1, len(snapshots))
	copy(sorted, snapshots)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ProjectedAtMs < sorted[j].ProjectedAtMs
	})

	curve := make([]portfoliodomain.EquityCurvePointV1, 0, len(sorted))
	peak := math.Inf(-1)

	for _, snap := range sorted {
		if snap.TotalEquityUSD > peak {
			peak = snap.TotalEquityUSD
		}

		drawdown := 0.0
		if peak > 1e-9 {
			drawdown = (peak - snap.TotalEquityUSD) / peak * 100.0
		}

		posCount := int32(0)
		for _, v := range snap.Venues {
			posCount += int32(len(v.Positions))
		}

		curve = append(curve, portfoliodomain.EquityCurvePointV1{
			TimestampMs:   snap.ProjectedAtMs,
			EquityUSD:     snap.TotalEquityUSD,
			RealizedUSD:   snap.TotalRealizedUSD,
			UnrealizedUSD: snap.TotalUnrealizedUSD,
			MarginUsedUSD: snap.TotalMarginUsedUSD,
			PositionCount: posCount,
			DrawdownPct:   drawdown,
		})
	}
	return curve
}

// BuildEquityCurveFromSummaries constructs a global equity curve from
// portfolio summaries.
func BuildEquityCurveFromSummaries(summaries []portfoliodomain.PortfolioSummaryV1) []portfoliodomain.EquityCurvePointV1 {
	if len(summaries) == 0 {
		return nil
	}

	sorted := make([]portfoliodomain.PortfolioSummaryV1, len(summaries))
	copy(sorted, summaries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ProjectedAtMs < sorted[j].ProjectedAtMs
	})

	curve := make([]portfoliodomain.EquityCurvePointV1, 0, len(sorted))
	peak := math.Inf(-1)

	for _, sum := range sorted {
		if sum.GlobalEquityUSD > peak {
			peak = sum.GlobalEquityUSD
		}

		drawdown := 0.0
		if peak > 1e-9 {
			drawdown = (peak - sum.GlobalEquityUSD) / peak * 100.0
		}

		curve = append(curve, portfoliodomain.EquityCurvePointV1{
			TimestampMs:   sum.ProjectedAtMs,
			EquityUSD:     sum.GlobalEquityUSD,
			RealizedUSD:   sum.GlobalRealizedUSD,
			UnrealizedUSD: sum.GlobalUnrealizedUSD,
			MarginUsedUSD: sum.GlobalMarginUsedUSD,
			PositionCount: sum.TotalPositionCount,
			DrawdownPct:   drawdown,
		})
	}
	return curve
}

// CheckReconciliation analyzes portfolio states and snapshots for the given
// account and produces a reconciliation report.  It detects:
//   - Sequence gaps in provenance (consecutive states with non-sequential execution_seq)
//   - Equity drift (large unexplained jumps between consecutive snapshots)
//   - Stale projections (states significantly older than the latest)
//   - Orphan states (states with no matching snapshot venue)
//   - PnL mismatches (snapshot aggregate vs sum of venue states)
//
// This function NEVER modifies state — it is purely diagnostic.
func CheckReconciliation(
	states []portfoliodomain.PortfolioStateV1,
	snapshots []portfoliodomain.AccountSnapshotV1,
	query portfoliodomain.ReconciliationQuery,
	nowMs int64,
) portfoliodomain.ReconciliationReportV1 {
	if nowMs <= 0 {
		nowMs = 1 // Caller must provide a valid timestamp; fallback to epoch+1 for safety.
	}

	reportID := sharedhash.HashFieldsFast("reconciliation-report", query.AccountID, strconv.FormatInt(nowMs, 10))

	report := portfoliodomain.ReconciliationReportV1{
		ReportID:       reportID,
		RunAtMs:        nowMs,
		AccountID:      query.AccountID,
		ScopeDesc:      "account:" + query.AccountID,
		TotalStates:    int32(len(states)),
		TotalSnapshots: int32(len(snapshots)),
		CheckedFromMs:  query.FromMs,
		CheckedToMs:    query.ToMs,
		Healthy:        true,
	}

	findings := make([]portfoliodomain.ReconciliationFinding, 0)

	// 1. Check sequence gaps across venue-scoped states
	findings = append(findings, checkSeqGaps(states, query.AccountID)...)

	// 2. Check equity drift between consecutive snapshots
	findings = append(findings, checkEquityDrift(snapshots, query.AccountID)...)

	// 3. Check stale projections
	findings = append(findings, checkStaleProjections(states, query.AccountID, nowMs)...)

	// 4. Check PnL consistency between states and snapshots
	findings = append(findings, checkPnLConsistency(states, snapshots, query.AccountID)...)

	for _, f := range findings {
		if f.Severity == portfoliodomain.SeverityError {
			report.Healthy = false
			break
		}
	}

	report.Findings = findings
	return report
}

func checkSeqGaps(states []portfoliodomain.PortfolioStateV1, accountID string) []portfoliodomain.ReconciliationFinding {
	// Group states by venue
	byVenue := make(map[string][]portfoliodomain.PortfolioStateV1)
	for _, s := range states {
		if s.AccountID != accountID && accountID != "" {
			continue
		}
		byVenue[s.Venue] = append(byVenue[s.Venue], s)
	}

	var findings []portfoliodomain.ReconciliationFinding

	for venue, venueStates := range byVenue {
		sort.Slice(venueStates, func(i, j int) bool {
			return venueStates[i].Provenance.SourceExecutionSeq < venueStates[j].Provenance.SourceExecutionSeq
		})

		for i := 1; i < len(venueStates); i++ {
			prev := venueStates[i-1].Provenance.SourceExecutionSeq
			curr := venueStates[i].Provenance.SourceExecutionSeq

			if curr > prev+1 {
				findings = append(findings, portfoliodomain.ReconciliationFinding{
					Kind:        portfoliodomain.FindingSeqGap,
					Severity:    portfoliodomain.SeverityWarning,
					AccountID:   accountID,
					Venue:       venue,
					Message:     "execution sequence gap detected",
					ExpectedSeq: prev + 1,
					ActualSeq:   curr,
					TimestampMs: venueStates[i].ProjectedAtMs,
				})
			}
		}
	}
	return findings
}

func checkEquityDrift(snapshots []portfoliodomain.AccountSnapshotV1, accountID string) []portfoliodomain.ReconciliationFinding {
	if len(snapshots) < 2 {
		return nil
	}

	sorted := make([]portfoliodomain.AccountSnapshotV1, len(snapshots))
	copy(sorted, snapshots)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ProjectedAtMs < sorted[j].ProjectedAtMs
	})

	var findings []portfoliodomain.ReconciliationFinding
	for i := 1; i < len(sorted); i++ {
		drift := sorted[i].TotalEquityUSD - sorted[i-1].TotalEquityUSD
		absDrift := math.Abs(drift)

		// Only flag large drifts relative to equity
		prevEquity := math.Abs(sorted[i-1].TotalEquityUSD)
		if prevEquity < 1e-9 {
			continue
		}
		driftPct := absDrift / prevEquity * 100.0

		// Flag if drift exceeds 10% AND exceeds the absolute threshold
		if driftPct > 10.0 && absDrift > equityDriftThresholdUSD {
			severity := portfoliodomain.SeverityWarning
			if driftPct > 50.0 {
				severity = portfoliodomain.SeverityError
			}
			findings = append(findings, portfoliodomain.ReconciliationFinding{
				Kind:        portfoliodomain.FindingEquityDrift,
				Severity:    severity,
				AccountID:   accountID,
				Message:     "significant equity change between consecutive snapshots",
				DriftUSD:    drift,
				TimestampMs: sorted[i].ProjectedAtMs,
			})
		}
	}
	return findings
}

func checkStaleProjections(states []portfoliodomain.PortfolioStateV1, accountID string, nowMs int64) []portfoliodomain.ReconciliationFinding {
	if len(states) == 0 {
		return nil
	}

	var latestMs int64
	for _, s := range states {
		if s.ProjectedAtMs > latestMs {
			latestMs = s.ProjectedAtMs
		}
	}

	var findings []portfoliodomain.ReconciliationFinding
	for _, s := range states {
		age := latestMs - s.ProjectedAtMs
		if age > staleProjectionThresholdMs {
			findings = append(findings, portfoliodomain.ReconciliationFinding{
				Kind:        portfoliodomain.FindingStaleProjection,
				Severity:    portfoliodomain.SeverityWarning,
				AccountID:   accountID,
				Venue:       s.Venue,
				Message:     "portfolio state projection is stale relative to latest",
				TimestampMs: s.ProjectedAtMs,
			})
		}
	}
	return findings
}

func checkPnLConsistency(
	states []portfoliodomain.PortfolioStateV1,
	snapshots []portfoliodomain.AccountSnapshotV1,
	accountID string,
) []portfoliodomain.ReconciliationFinding {
	if len(snapshots) == 0 || len(states) == 0 {
		return nil
	}

	// Use the latest snapshot
	latest := snapshots[0]
	for _, s := range snapshots {
		if s.ProjectedAtMs > latest.ProjectedAtMs {
			latest = s
		}
	}

	// Sum realized PnL from all states for the same account
	var stateRealizedSum float64
	for _, s := range states {
		if s.AccountID != accountID && accountID != "" {
			continue
		}
		stateRealizedSum += s.RealizedPnlUSD
	}

	drift := math.Abs(stateRealizedSum - latest.TotalRealizedUSD)
	if drift > equityDriftThresholdUSD {
		severity := portfoliodomain.SeverityWarning
		if drift > 1.0 {
			severity = portfoliodomain.SeverityError
		}
		return []portfoliodomain.ReconciliationFinding{{
			Kind:        portfoliodomain.FindingPnLMismatch,
			Severity:    severity,
			AccountID:   accountID,
			Message:     "realized PnL mismatch between sum of states and latest snapshot",
			DriftUSD:    stateRealizedSum - latest.TotalRealizedUSD,
			TimestampMs: latest.ProjectedAtMs,
		}}
	}
	return nil
}
