package problem_test

import (
	"errors"
	"testing"

	"github.com/market-raccoon/internal/shared/problem"
)

func TestNew(t *testing.T) {
	p := problem.New(problem.ValidationFailed, "field required")
	if p.Code != problem.ValidationFailed {
		t.Errorf("expected code %s, got %s", problem.ValidationFailed, p.Code)
	}
	if p.Message != "field required" {
		t.Errorf("unexpected message: %s", p.Message)
	}
	if p.Details == nil {
		t.Error("Details must be non-nil")
	}
	if p.Retryable {
		t.Error("should not be retryable by default")
	}
}

func TestError(t *testing.T) {
	tests := []struct {
		name string
		p    *problem.Problem
		want string
	}{
		{
			name: "no cause",
			p:    problem.New(problem.NotFound, "item missing"),
			want: "SYS_NOT_FOUND: item missing",
		},
		{
			name: "with cause",
			p:    problem.Wrap(errors.New("db error"), problem.Internal, "query failed"),
			want: "SYS_INTERNAL: query failed: db error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.Error(); got != tc.want {
				t.Errorf("Error() = %q; want %q", got, tc.want)
			}
		})
	}
}

func TestWithDetail(t *testing.T) {
	p := problem.New(problem.ValidationFailed, "bad input")
	p2 := problem.WithDetail(p, "field", "price")
	p3 := problem.WithDetail(p2, "min", 0.0)

	if p2.Details["field"] != "price" {
		t.Error("detail not set")
	}
	// original must be immutable
	if _, ok := p.Details["field"]; ok {
		t.Error("original should not be mutated")
	}
	if p3.Details["field"] != "price" {
		t.Error("detail chain broken")
	}
	if p3.Details["min"] != 0.0 {
		t.Error("second detail not set")
	}
}

func TestWrap(t *testing.T) {
	cause := errors.New("root cause")
	p := problem.Wrap(cause, problem.Internal, "wrapped")

	if !errors.Is(p, cause) {
		t.Error("errors.Is should traverse cause")
	}
	if p.Cause != cause {
		t.Error("Cause not set")
	}
}

func TestWithRetryable(t *testing.T) {
	p := problem.WithRetryable(problem.New(problem.Internal, "transient"))
	if !p.Retryable {
		t.Error("should be retryable")
	}
}

func TestNewf(t *testing.T) {
	p := problem.Newf(problem.InvalidArgument, "value %d out of range", 42)
	if p.Message != "value 42 out of range" {
		t.Errorf("unexpected message: %s", p.Message)
	}
}

func TestCodeCategories(t *testing.T) {
	tests := []struct {
		code   problem.ProblemCode
		prefix string
	}{
		{problem.ValidationFailed, "VAL_"},
		{problem.InvalidArgument, "VAL_"},
		{problem.NotFound, "SYS_"},
		{problem.Conflict, "SYS_"},
		{problem.Internal, "SYS_"},
		{problem.Unavailable, "SYS_"},
		{problem.OutOfOrder, "MD_"},
		{problem.Duplicate, "MD_"},
		{problem.IntegrityViolation, "AGG_"},
	}
	for _, tc := range tests {
		t.Run(string(tc.code), func(t *testing.T) {
			s := string(tc.code)
			if len(s) < len(tc.prefix) || s[:len(tc.prefix)] != tc.prefix {
				t.Errorf("code %q does not start with prefix %q", s, tc.prefix)
			}
		})
	}
}
