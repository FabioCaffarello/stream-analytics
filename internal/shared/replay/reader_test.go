package replay

import (
	"testing"

	"github.com/market-raccoon/internal/shared/problem"
)

func TestAnnotateLine_NilProblem(t *testing.T) {
	if got := annotateLine(nil, 10); got != nil {
		t.Fatalf("expected nil when input problem is nil, got %v", got)
	}
}

func TestAnnotateLine_AddsLineDetailAsString(t *testing.T) {
	base := problem.New(problem.Internal, "parse error")
	out := annotateLine(base, 42)
	if out == nil {
		t.Fatal("expected non-nil annotated problem")
	}
	v, ok := out.Details["line"]
	if !ok {
		t.Fatal("expected detail 'line' to be present")
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("expected detail 'line' to be string, got %T", v)
	}
	if s != "42" {
		t.Fatalf("expected line detail '42', got %q", s)
	}
}
