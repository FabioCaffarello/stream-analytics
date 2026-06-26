package domain

import (
	"strings"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// SessionAnchorKind classifies the session boundary strategy.
type SessionAnchorKind string

const (
	SessionAnchorExchange SessionAnchorKind = "exchange"
	SessionAnchorUTC      SessionAnchorKind = "utc"
	SessionAnchorCustom   SessionAnchorKind = "custom"
)

// SessionAnchor defines a repeating session boundary.
// Exchange anchors use timezone-aware open/close rules.
// UTC anchors floor to day boundaries.
// Custom anchors floor to arbitrary duration from epoch.
type SessionAnchor struct {
	Kind       SessionAnchorKind `json:"kind"`
	Label      string            `json:"label"`
	Timezone   string            `json:"timezone"`
	OpenHour   int               `json:"open_hour"`
	OpenMinute int               `json:"open_minute"`
	DurationMs int64             `json:"duration_ms"`
}

func (a SessionAnchor) Validate() *problem.Problem {
	if strings.TrimSpace(a.Label) == "" {
		return problem.New(problem.ValidationFailed, "session anchor label must not be empty")
	}
	switch a.Kind {
	case SessionAnchorExchange:
		if strings.TrimSpace(a.Timezone) == "" {
			return problem.New(problem.ValidationFailed, "exchange session anchor requires timezone")
		}
		if _, err := time.LoadLocation(a.Timezone); err != nil {
			return problem.Newf(problem.ValidationFailed, "invalid timezone: %s", a.Timezone)
		}
		if a.DurationMs <= 0 {
			return problem.New(problem.ValidationFailed, "exchange session anchor requires positive duration_ms")
		}
	case SessionAnchorUTC:
		if a.DurationMs <= 0 {
			return problem.New(problem.ValidationFailed, "utc session anchor requires positive duration_ms")
		}
	case SessionAnchorCustom:
		if a.DurationMs <= 0 {
			return problem.New(problem.ValidationFailed, "custom session anchor requires positive duration_ms")
		}
	default:
		return problem.Newf(problem.ValidationFailed, "unknown session anchor kind: %s", a.Kind)
	}
	return nil
}

var SessionPresets = map[string]SessionAnchor{
	"CME_RTH": {
		Kind: SessionAnchorExchange, Label: "CME_RTH",
		Timezone: "America/Chicago", OpenHour: 8, OpenMinute: 30,
		DurationMs: 6*3600*1000 + 45*60*1000, // 6h45m
	},
	"CME_ETH": {
		Kind: SessionAnchorExchange, Label: "CME_ETH",
		Timezone: "America/Chicago", OpenHour: 17, OpenMinute: 0,
		DurationMs: 15*3600*1000 + 30*60*1000, // 15h30m
	},
	"ASIA": {
		Kind: SessionAnchorUTC, Label: "ASIA",
		Timezone: "UTC", OpenHour: 0, OpenMinute: 0,
		DurationMs: 8 * 3600 * 1000, // 8h
	},
	"LONDON": {
		Kind: SessionAnchorUTC, Label: "LONDON",
		Timezone: "UTC", OpenHour: 8, OpenMinute: 0,
		DurationMs: 8*3600*1000 + 30*60*1000, // 8h30m
	},
	"NY": {
		Kind: SessionAnchorUTC, Label: "NY",
		Timezone: "UTC", OpenHour: 13, OpenMinute: 30,
		DurationMs: 6*3600*1000 + 30*60*1000, // 6h30m
	},
	"UTC_DAILY": {
		Kind: SessionAnchorUTC, Label: "UTC_DAILY",
		Timezone: "UTC", OpenHour: 0, OpenMinute: 0,
		DurationMs: 24 * 3600 * 1000, // 24h
	},
	"CRYPTO_4H": {
		Kind: SessionAnchorCustom, Label: "CRYPTO_4H",
		Timezone:   "UTC",
		DurationMs: 4 * 3600 * 1000, // 4h
	},
}

// ResolveSessionBounds computes the concrete [startMs, endMs) for the session
// containing refTimeMs. Deterministic: same anchor + refTime = same result.
func ResolveSessionBounds(anchor SessionAnchor, refTimeMs int64) (startMs, endMs int64, p *problem.Problem) {
	if refTimeMs <= 0 {
		return 0, 0, problem.New(problem.ValidationFailed, "refTimeMs must be positive")
	}
	if p := anchor.Validate(); p != nil {
		return 0, 0, p
	}

	switch anchor.Kind {
	case SessionAnchorCustom:
		// Floor to duration boundary from epoch.
		startMs = (refTimeMs / anchor.DurationMs) * anchor.DurationMs
		return startMs, startMs + anchor.DurationMs, nil

	case SessionAnchorUTC:
		loc := time.UTC
		return resolveTimezoneSession(anchor, loc, refTimeMs)

	case SessionAnchorExchange:
		loc, err := time.LoadLocation(anchor.Timezone)
		if err != nil {
			return 0, 0, problem.Newf(problem.ValidationFailed, "invalid timezone: %s", anchor.Timezone)
		}
		return resolveTimezoneSession(anchor, loc, refTimeMs)
	}
	return 0, 0, problem.New(problem.ValidationFailed, "unreachable")
}

func resolveTimezoneSession(anchor SessionAnchor, loc *time.Location, refTimeMs int64) (int64, int64, *problem.Problem) {
	ref := time.UnixMilli(refTimeMs).In(loc)
	// Build today's session open.
	todayOpen := time.Date(ref.Year(), ref.Month(), ref.Day(), anchor.OpenHour, anchor.OpenMinute, 0, 0, loc)
	todayOpenMs := todayOpen.UnixMilli()
	todayEndMs := todayOpenMs + anchor.DurationMs

	if refTimeMs >= todayOpenMs && refTimeMs < todayEndMs {
		return todayOpenMs, todayEndMs, nil
	}
	if refTimeMs < todayOpenMs {
		// Session hasn't opened yet today — use yesterday's.
		yesterdayOpen := todayOpen.AddDate(0, 0, -1)
		yMs := yesterdayOpen.UnixMilli()
		return yMs, yMs + anchor.DurationMs, nil
	}
	// Past today's close — use today's (closed session).
	return todayOpenMs, todayEndMs, nil
}
