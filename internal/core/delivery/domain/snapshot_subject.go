package domain

import "github.com/market-raccoon/internal/shared/problem"

const snapshotStreamType = "aggregation.snapshot"

// SnapshotSubject creates the canonical WS subject for aggregation snapshots.
func SnapshotSubject(venue, instrument, timeframe string) (Subject, *problem.Problem) {
	return NewSubject(snapshotStreamType, venue, instrument, timeframe)
}
