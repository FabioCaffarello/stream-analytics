// Package result provides a typed success/failure result container.
package result

import "github.com/market-raccoon/internal/shared/problem"

// Result is a discriminated union: either a value T or a Problem.
// It is never both and never neither.
type Result[T any] struct {
	ok      bool
	value   T
	problem *problem.Problem
}

// Ok constructs a successful Result.
func Ok[T any](v T) Result[T] {
	return Result[T]{ok: true, value: v}
}

// Fail constructs a failure Result from a code and message.
func Fail[T any](code problem.ProblemCode, msg string) Result[T] {
	return Result[T]{problem: problem.New(code, msg)}
}

// Failf constructs a failure Result from a formatted message.
func Failf[T any](code problem.ProblemCode, format string, args ...any) Result[T] {
	return Result[T]{problem: problem.Newf(code, format, args...)}
}

// FailProblem wraps an existing Problem into a failure Result.
func FailProblem[T any](p *problem.Problem) Result[T] {
	return Result[T]{problem: p}
}

// IsOk reports whether the result is successful.
func (r Result[T]) IsOk() bool { return r.ok }

// IsFail reports whether the result is a failure.
func (r Result[T]) IsFail() bool { return !r.ok }

// Value returns the success value. Zero value if failed.
func (r Result[T]) Value() T { return r.value }

// Problem returns the problem. Nil if successful.
func (r Result[T]) Problem() *problem.Problem { return r.problem }

// Unwrap returns (value, nil) or (zero, problem).
// Convenient for callers that prefer the (T, error) idiom.
func (r Result[T]) Unwrap() (T, *problem.Problem) {
	return r.value, r.problem
}

// Map applies f to the value if Ok, propagating Fail unchanged.
func Map[T, U any](r Result[T], f func(T) U) Result[U] {
	if r.IsFail() {
		return FailProblem[U](r.problem)
	}
	return Ok(f(r.value))
}

// Bind applies f (which may itself fail) to the value if Ok.
func Bind[T, U any](r Result[T], f func(T) Result[U]) Result[U] {
	if r.IsFail() {
		return FailProblem[U](r.problem)
	}
	return f(r.value)
}
