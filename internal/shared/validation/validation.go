// Package validation provides reusable input/domain validation helpers.
package validation

import (
	"strconv"
	"strings"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// NonEmptyString returns a Problem if value is empty after trimming.
func NonEmptyString(fieldName, value string) *problem.Problem {
	if strings.TrimSpace(value) == "" {
		return problem.WithDetail(
			problem.Newf(problem.ValidationFailed, "%s must not be empty", fieldName),
			"field", fieldName,
		)
	}
	return nil
}

// MaxLength returns a Problem if value exceeds max characters.
func MaxLength(fieldName, value string, max int) *problem.Problem {
	if len(value) > max {
		return problem.WithDetail(
			problem.WithDetail(
				problem.Newf(problem.ValidationFailed, "%s must not exceed %d characters", fieldName, max),
				"field", fieldName,
			),
			"max", max,
		)
	}
	return nil
}

// Positive returns a Problem if value is not strictly positive.
func Positive(fieldName string, value float64) *problem.Problem {
	if value <= 0 {
		return problem.WithDetail(
			problem.WithDetail(
				problem.Newf(problem.ValidationFailed, "%s must be positive, got %s", fieldName, formatNum(value)),
				"field", fieldName,
			),
			"value", value,
		)
	}
	return nil
}

// NonNegative returns a Problem if value is negative.
func NonNegative(fieldName string, value float64) *problem.Problem {
	if value < 0 {
		return problem.WithDetail(
			problem.WithDetail(
				problem.Newf(problem.ValidationFailed, "%s must be non-negative, got %s", fieldName, formatNum(value)),
				"field", fieldName,
			),
			"value", value,
		)
	}
	return nil
}

// WithinRangeInclusive returns a Problem if value is outside [min, max].
func WithinRangeInclusive(fieldName string, value, min, max float64) *problem.Problem {
	if value < min || value > max {
		return problem.WithDetail(
			problem.WithDetail(
				problem.WithDetail(
					problem.Newf(problem.ValidationFailed,
						"%s must be in [%s, %s], got %s",
						fieldName, formatNum(min), formatNum(max), formatNum(value)),
					"field", fieldName,
				),
				"min", min,
			),
			"max", max,
		)
	}
	return nil
}

// OneOf returns a Problem if value is not in the allowed set.
func OneOf(fieldName, value string, allowed []string) *problem.Problem {
	for _, a := range allowed {
		if a == value {
			return nil
		}
	}
	return problem.WithDetail(
		problem.WithDetail(
			problem.Newf(problem.ValidationFailed, "%s must be one of %v, got %q", fieldName, allowed, value),
			"field", fieldName,
		),
		"value", value,
	)
}

// PositiveInt returns a Problem if value is not strictly positive.
func PositiveInt(fieldName string, value int64) *problem.Problem {
	if value <= 0 {
		return problem.WithDetail(
			problem.WithDetail(
				problem.Newf(problem.ValidationFailed, "%s must be positive, got %d", fieldName, value),
				"field", fieldName,
			),
			"value", value,
		)
	}
	return nil
}

// NonNegativeInt returns a Problem if value is negative.
func NonNegativeInt(fieldName string, value int64) *problem.Problem {
	if value < 0 {
		return problem.WithDetail(
			problem.WithDetail(
				problem.Newf(problem.ValidationFailed, "%s must be non-negative, got %d", fieldName, value),
				"field", fieldName,
			),
			"value", value,
		)
	}
	return nil
}

// NonEmptySliceLen returns a Problem if length == 0.
// Use when you have a slice of any type — pass len(slice) as length.
func NonEmptySliceLen(fieldName string, length int) *problem.Problem {
	if length == 0 {
		return problem.WithDetail(
			problem.Newf(problem.ValidationFailed, "%s must not be empty", fieldName),
			"field", fieldName,
		)
	}
	return nil
}

// Collect evaluates pre-computed Problem pointers and returns the first non-nil one.
// Use when all validators have already been evaluated (eager).
func Collect(validators ...*problem.Problem) *problem.Problem {
	for _, p := range validators {
		if p != nil {
			return p
		}
	}
	return nil
}

// Combine evaluates lazy validator functions and returns the first non-nil Problem.
// Unlike Collect, the functions are called in order and evaluation stops on first failure.
// Use when validator construction itself has side effects or is expensive.
func Combine(validators ...func() *problem.Problem) *problem.Problem {
	for _, fn := range validators {
		if p := fn(); p != nil {
			return p
		}
	}
	return nil
}

func formatNum(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
