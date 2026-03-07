package replay

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

// ValidationHook runs after replay completes with the output summary and captured envelopes.
type ValidationHook func(summary ReplaySummary, outputs []envelope.Envelope) *problem.Problem

// ExpectOutputCount validates that the replay produced exactly n output envelopes.
func ExpectOutputCount(n int) ValidationHook {
	return func(_ ReplaySummary, outputs []envelope.Envelope) *problem.Problem {
		if len(outputs) != n {
			return problem.Newf(problem.ValidationFailed,
				"output count mismatch: expected=%d actual=%d", n, len(outputs))
		}
		return nil
	}
}

// ExpectOutputSHA validates the SHA-256 chain hash of all output payloads.
func ExpectOutputSHA(expected string) ValidationHook {
	return func(_ ReplaySummary, outputs []envelope.Envelope) *problem.Problem {
		h := sha256.New()
		for i := range outputs {
			_, _ = h.Write(outputs[i].Payload)
			_, _ = h.Write([]byte{'\n'})
		}
		got := hex.EncodeToString(h.Sum(nil))
		if !strings.EqualFold(strings.TrimSpace(expected), strings.TrimSpace(got)) {
			return problem.Newf(problem.ValidationFailed,
				"output SHA mismatch: expected=%s actual=%s", expected, got)
		}
		return nil
	}
}

// ExpectNoDrift validates that outputs match golden envelopes with no breaking changes.
// Additive-only changes (new fields) are allowed and do not cause failure.
func ExpectNoDrift(goldenOutputs []envelope.Envelope) ValidationHook {
	return func(_ ReplaySummary, outputs []envelope.Envelope) *problem.Problem {
		results, p := DetectDrift(outputs, goldenOutputs)
		if p != nil {
			return p
		}
		for i := range results {
			if !results[i].Compatible {
				return problem.Newf(problem.ValidationFailed,
					"breaking drift at envelope[%d]: %d mismatches, %d dropped fields",
					results[i].EnvelopeIndex,
					len(results[i].FieldMismatches),
					len(results[i].DroppedFields))
			}
		}
		return nil
	}
}

// ExpectNoFieldMismatch validates that outputs match golden envelopes exactly.
// Even additive changes are flagged.
func ExpectNoFieldMismatch(goldenOutputs []envelope.Envelope) ValidationHook {
	return func(_ ReplaySummary, outputs []envelope.Envelope) *problem.Problem {
		results, p := DetectDrift(outputs, goldenOutputs)
		if p != nil {
			return p
		}
		if len(results) > 0 {
			r := results[0]
			return problem.Newf(problem.ValidationFailed,
				"field drift at envelope[%d]: %d mismatches, %d new, %d dropped",
				r.EnvelopeIndex,
				len(r.FieldMismatches),
				len(r.NewFields),
				len(r.DroppedFields))
		}
		return nil
	}
}

// ExpectInputSHA validates the input fixture chain hash matches expected.
func ExpectInputSHA(expected string) ValidationHook {
	return func(summary ReplaySummary, _ []envelope.Envelope) *problem.Problem {
		if !strings.EqualFold(strings.TrimSpace(expected), strings.TrimSpace(summary.InputSHA)) {
			return problem.Newf(problem.ValidationFailed,
				"input SHA mismatch: expected=%s actual=%s", expected, summary.InputSHA)
		}
		return nil
	}
}

// RunValidations executes a set of validation hooks against a replay result.
func RunValidations(summary ReplaySummary, outputs []envelope.Envelope, hooks []ValidationHook) *problem.Problem {
	for i, hook := range hooks {
		if p := hook(summary, outputs); p != nil {
			return problem.WithDetail(p, "validation_hook_index", i)
		}
	}
	return nil
}
