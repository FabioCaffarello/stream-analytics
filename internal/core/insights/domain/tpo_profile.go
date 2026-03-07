package domain

import (
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
)

const (
	TPOProfileType    = "insights.tpo_profile"
	TPOProfileVersion = 1

	TPOMaxPeriods     = 24 // A..X
	TPOMaxLevels      = 400
	TPOPeriodDuration = 30 * 60 * 1000 // 30 minutes in ms
)

// TPOProfileV1 is a Time-Price Opportunity profile for a session.
type TPOProfileV1 struct {
	Venue         string        `json:"venue"`
	Instrument    string        `json:"instrument"`
	Anchor        SessionAnchor `json:"anchor"`
	Periods       []TPOPeriod   `json:"periods"`
	Levels        []TPOLevel    `json:"levels"`
	POCPrice      float64       `json:"poc_price"`
	ValueAreaHigh float64       `json:"value_area_high"`
	ValueAreaLow  float64       `json:"value_area_low"`
	IBHigh        float64       `json:"ib_high"` // Initial Balance high (periods A+B)
	IBLow         float64       `json:"ib_low"`  // Initial Balance low  (periods A+B)
	RangeHigh     float64       `json:"range_high"`
	RangeLow      float64       `json:"range_low"`
	WindowStartTs int64         `json:"window_start_ts"`
	WindowEndTs   int64         `json:"window_end_ts"`
}

// TPOPeriod represents one 30-minute slice within a session.
type TPOPeriod struct {
	Letter    byte    `json:"letter"` // 'A'..'X'
	StartMs   int64   `json:"start_ms"`
	EndMs     int64   `json:"end_ms"`
	HighPrice float64 `json:"high_price"`
	LowPrice  float64 `json:"low_price"`
}

// TPOLevel represents a price level with the set of period letters that traded there.
type TPOLevel struct {
	PriceLow  float64 `json:"price_low"`
	PriceHigh float64 `json:"price_high"`
	Letters   []byte  `json:"letters"`
	Count     int     `json:"count"`
}

func (t TPOProfileV1) Validate() *problem.Problem {
	if strings.TrimSpace(t.Venue) == "" || strings.TrimSpace(t.Instrument) == "" {
		return problem.New(problem.ValidationFailed, "tpo venue/instrument must not be empty")
	}
	if p := t.Anchor.Validate(); p != nil {
		return p
	}
	if t.WindowStartTs <= 0 || t.WindowEndTs <= t.WindowStartTs {
		return problem.New(problem.ValidationFailed, "tpo window bounds are invalid")
	}
	if p := t.validatePeriodsAndLevels(); p != nil {
		return p
	}
	if !isFiniteFloat(t.ValueAreaLow) || !isFiniteFloat(t.ValueAreaHigh) || t.ValueAreaHigh < t.ValueAreaLow {
		return problem.New(problem.ValidationFailed, "tpo value area bounds are invalid")
	}
	return nil
}

func (t TPOProfileV1) validatePeriodsAndLevels() *problem.Problem {
	if len(t.Periods) == 0 {
		return problem.New(problem.ValidationFailed, "tpo requires at least one period")
	}
	if len(t.Periods) > TPOMaxPeriods {
		return problem.New(problem.ValidationFailed, "tpo period cap exceeded")
	}
	if len(t.Levels) == 0 {
		return problem.New(problem.ValidationFailed, "tpo requires at least one level")
	}
	if len(t.Levels) > TPOMaxLevels {
		return problem.New(problem.ValidationFailed, "tpo level cap exceeded")
	}
	if !t.pocMatchesLevels() {
		return problem.New(problem.ValidationFailed, "tpo poc_price must match level with most letters")
	}
	return nil
}

func (t TPOProfileV1) pocMatchesLevels() bool {
	maxCount := 0
	for _, lv := range t.Levels {
		if lv.Count > maxCount {
			maxCount = lv.Count
		}
	}
	for _, lv := range t.Levels {
		if lv.Count == maxCount && lv.PriceLow == t.POCPrice {
			return true
		}
	}
	return false
}

// PeriodLetter returns the letter for a given period index (0='A', 23='X').
func PeriodLetter(idx int) byte {
	if idx < 0 || idx >= TPOMaxPeriods {
		return '?'
	}
	return byte('A') + byte(idx)
}

// PeriodIndex returns the 0-based period index for a timestamp within a session.
func PeriodIndex(sessionStartMs, tsMs int64) int {
	if tsMs < sessionStartMs {
		return 0
	}
	idx := int((tsMs - sessionStartMs) / int64(TPOPeriodDuration))
	if idx >= TPOMaxPeriods {
		return TPOMaxPeriods - 1
	}
	return idx
}
