package replay

import (
	"context"
	"strings"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	sharedhash "github.com/FabioCaffarello/stream-analytics/internal/shared/hash"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// Source streams envelopes for replay recording.
type Source interface {
	Read(ctx context.Context) (<-chan envelope.Envelope, func() error, *problem.Problem)
}

// SourceRecordSummary captures deterministic source->fixture recording stats.
type SourceRecordSummary struct {
	ReadCount    int
	WrittenCount int
	OutputSHA    string
}

// RecordFromSource records envelopes from src into deterministic replay fixture JSONL.
// maxN<=0 means unlimited; until.IsZero() disables ts_ingest upper-bound filter.
//
//nolint:gocyclo // Function keeps cancellation/error/flush semantics explicit for replay safety.
func RecordFromSource(ctx context.Context, src Source, outPath string, maxN int, until time.Time) (SourceRecordSummary, *problem.Problem) {
	if src == nil {
		return SourceRecordSummary{}, problem.New(problem.ValidationFailed, "source must not be nil")
	}
	if strings.TrimSpace(outPath) == "" {
		return SourceRecordSummary{}, problem.WithDetail(problem.New(problem.ValidationFailed, "output path must not be empty"), "field", "out_path")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	ch, closeFn, p := src.Read(runCtx)
	if p != nil {
		return SourceRecordSummary{}, p
	}
	if closeFn == nil {
		return SourceRecordSummary{}, problem.New(problem.ValidationFailed, "source close function must not be nil")
	}

	writer, p := NewWriter(outPath)
	if p != nil {
		_ = closeFn()
		return SourceRecordSummary{}, p
	}

	summary := SourceRecordSummary{}
	hashes := make([]string, 0, 1024)
	untilMillis := int64(0)
	if !until.IsZero() {
		untilMillis = until.UnixMilli()
	}

	for env := range ch {
		summary.ReadCount++
		if untilMillis > 0 && env.TsIngest > untilMillis {
			continue
		}

		if p := writer.Append(env); p != nil {
			_ = writer.Close()
			_ = closeFn()
			return SourceRecordSummary{}, p
		}
		summary.WrittenCount++

		base, p := makeFixtureBaseFromEnvelope(env)
		if p != nil {
			_ = writer.Close()
			_ = closeFn()
			return SourceRecordSummary{}, p
		}
		baseCanonical, p := canonicalBaseBytes(base)
		if p != nil {
			_ = writer.Close()
			_ = closeFn()
			return SourceRecordSummary{}, p
		}
		hashes = append(hashes, lineSHA256(baseCanonical))

		if maxN > 0 && summary.WrittenCount >= maxN {
			cancel()
			break
		}
	}

	if p := writer.Close(); p != nil {
		_ = closeFn()
		return SourceRecordSummary{}, p
	}
	if err := closeFn(); err != nil {
		return SourceRecordSummary{}, problem.WithRetryable(problem.Wrap(err, problem.Unavailable, "close source failed"))
	}

	summary.OutputSHA = sharedhash.HashFieldsFast(hashes...)
	return summary, nil
}
