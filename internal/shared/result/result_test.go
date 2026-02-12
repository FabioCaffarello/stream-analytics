package result_test

import (
	"testing"

	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/result"
)

func TestOk(t *testing.T) {
	r := result.Ok(42)
	if !r.IsOk() {
		t.Fatal("expected Ok")
	}
	if r.IsFail() {
		t.Fatal("should not be Fail")
	}
	if r.Value() != 42 {
		t.Errorf("value = %d; want 42", r.Value())
	}
	if r.Problem() != nil {
		t.Error("problem should be nil on Ok")
	}
}

func TestFail(t *testing.T) {
	r := result.Fail[int](problem.NotFound, "missing")
	if r.IsOk() {
		t.Fatal("expected Fail")
	}
	if r.Problem() == nil {
		t.Fatal("problem must not be nil on Fail")
	}
	if r.Problem().Code != problem.NotFound {
		t.Errorf("code = %s; want NOT_FOUND", r.Problem().Code)
	}
	if r.Value() != 0 {
		t.Error("zero value expected on failure")
	}
}

func TestFailProblem(t *testing.T) {
	p := problem.New(problem.Conflict, "dup")
	r := result.FailProblem[string](p)
	if r.Problem() != p {
		t.Error("problem pointer not preserved")
	}
}

func TestMap(t *testing.T) {
	t.Run("propagates value", func(t *testing.T) {
		r := result.Ok(10)
		r2 := result.Map(r, func(v int) string { return "val" })
		if !r2.IsOk() || r2.Value() != "val" {
			t.Errorf("unexpected Map result")
		}
	})

	t.Run("propagates fail", func(t *testing.T) {
		r := result.Fail[int](problem.Internal, "err")
		r2 := result.Map(r, func(v int) string { return "val" })
		if r2.IsOk() {
			t.Error("fail should propagate")
		}
		if r2.Problem().Code != problem.Internal {
			t.Error("problem code should propagate")
		}
	})
}

func TestBind(t *testing.T) {
	t.Run("chains ok", func(t *testing.T) {
		r := result.Ok(5)
		r2 := result.Bind(r, func(v int) result.Result[int] {
			return result.Ok(v * 2)
		})
		if !r2.IsOk() || r2.Value() != 10 {
			t.Errorf("unexpected Bind result")
		}
	})

	t.Run("short circuits on fail", func(t *testing.T) {
		r := result.Fail[int](problem.ValidationFailed, "bad")
		called := false
		r2 := result.Bind(r, func(v int) result.Result[int] {
			called = true
			return result.Ok(v)
		})
		if called {
			t.Error("function should not be called on fail")
		}
		if r2.IsOk() {
			t.Error("should still fail")
		}
	})
}

func TestUnwrap(t *testing.T) {
	v, p := result.Ok("hello").Unwrap()
	if v != "hello" || p != nil {
		t.Error("unexpected Unwrap on Ok")
	}

	var zero string
	v2, p2 := result.Fail[string](problem.Internal, "x").Unwrap()
	if v2 != zero || p2 == nil {
		t.Error("unexpected Unwrap on Fail")
	}
}
