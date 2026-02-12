package validation_test

import (
	"testing"

	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/validation"
)

func TestNonEmptyString(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"hello", false},
		{"", true},
		{"   ", true},
		{"\t\n", true},
	}
	for _, tc := range tests {
		p := validation.NonEmptyString("field", tc.input)
		if tc.wantErr && p == nil {
			t.Errorf("input=%q: expected problem, got nil", tc.input)
		}
		if !tc.wantErr && p != nil {
			t.Errorf("input=%q: unexpected problem %s", tc.input, p)
		}
	}
}

func TestPositive(t *testing.T) {
	tests := []struct {
		value   float64
		wantErr bool
	}{
		{1.0, false},
		{0.001, false},
		{0.0, true},
		{-1.0, true},
	}
	for _, tc := range tests {
		p := validation.Positive("price", tc.value)
		if tc.wantErr && p == nil {
			t.Errorf("value=%v: expected problem, got nil", tc.value)
		}
		if !tc.wantErr && p != nil {
			t.Errorf("value=%v: unexpected problem %s", tc.value, p)
		}
	}
}

func TestNonNegative(t *testing.T) {
	tests := []struct {
		value   float64
		wantErr bool
	}{
		{0.0, false},
		{5.0, false},
		{-0.001, true},
	}
	for _, tc := range tests {
		p := validation.NonNegative("qty", tc.value)
		if tc.wantErr && p == nil {
			t.Errorf("value=%v: expected problem, got nil", tc.value)
		}
		if !tc.wantErr && p != nil {
			t.Errorf("value=%v: unexpected problem %s", tc.value, p)
		}
	}
}

func TestOneOf(t *testing.T) {
	allowed := []string{"buy", "sell"}
	tests := []struct {
		value   string
		wantErr bool
	}{
		{"buy", false},
		{"sell", false},
		{"hold", true},
		{"", true},
	}
	for _, tc := range tests {
		p := validation.OneOf("side", tc.value, allowed)
		if tc.wantErr && p == nil {
			t.Errorf("value=%q: expected problem, got nil", tc.value)
		}
		if !tc.wantErr && p != nil {
			t.Errorf("value=%q: unexpected problem %s", tc.value, p)
		}
	}
}

func TestCollect(t *testing.T) {
	t.Run("no problems", func(t *testing.T) {
		p := validation.Collect(nil, nil, nil)
		if p != nil {
			t.Error("expected nil")
		}
	})

	t.Run("returns first problem", func(t *testing.T) {
		first := problem.New(problem.ValidationFailed, "first")
		second := problem.New(problem.ValidationFailed, "second")
		p := validation.Collect(nil, first, second)
		if p != first {
			t.Error("should return first non-nil problem")
		}
	})
}

func TestCombine(t *testing.T) {
	t.Run("all pass", func(t *testing.T) {
		p := validation.Combine(
			func() *problem.Problem { return nil },
			func() *problem.Problem { return nil },
		)
		if p != nil {
			t.Error("expected nil")
		}
	})

	t.Run("stops at first failure", func(t *testing.T) {
		calls := 0
		first := problem.New(problem.ValidationFailed, "first")
		p := validation.Combine(
			func() *problem.Problem { calls++; return first },
			func() *problem.Problem { calls++; return nil },
		)
		if p != first {
			t.Error("should return first problem")
		}
		if calls != 1 {
			t.Errorf("should stop after first failure, got %d calls", calls)
		}
	})
}

func TestWithinRangeInclusive(t *testing.T) {
	tests := []struct {
		value   float64
		min     float64
		max     float64
		wantErr bool
	}{
		{0.5, 0, 1, false},
		{0.0, 0, 1, false},
		{1.0, 0, 1, false},
		{-0.1, 0, 1, true},
		{1.1, 0, 1, true},
	}
	for _, tc := range tests {
		p := validation.WithinRangeInclusive("confidence", tc.value, tc.min, tc.max)
		if tc.wantErr && p == nil {
			t.Errorf("value=%v: expected problem, got nil", tc.value)
		}
		if !tc.wantErr && p != nil {
			t.Errorf("value=%v: unexpected problem %s", tc.value, p)
		}
	}
}

func TestNonEmptySliceLen(t *testing.T) {
	if p := validation.NonEmptySliceLen("items", 0); p == nil {
		t.Error("expected problem for empty slice")
	}
	if p := validation.NonEmptySliceLen("items", 1); p != nil {
		t.Errorf("unexpected problem: %s", p)
	}
}
