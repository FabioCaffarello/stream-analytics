package domain

import (
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
)

var timeframeToMs = map[string]int64{
	"1s":  1_000,
	"5s":  5_000,
	"1m":  60_000,
	"5m":  300_000,
	"15m": 900_000,
	"30m": 1_800_000,
	"1h":  3_600_000,
	"4h":  14_400_000,
	"1d":  86_400_000,
}

// TimeframeToMs converts a human timeframe string to milliseconds.
func TimeframeToMs(tf string) (int64, *problem.Problem) {
	normalized := strings.ToLower(strings.TrimSpace(tf))
	ms, ok := timeframeToMs[normalized]
	if !ok {
		return 0, problem.Newf(problem.ValidationFailed, "unsupported timeframe %q", tf)
	}
	return ms, nil
}
