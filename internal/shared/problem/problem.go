// Package problem defines the canonical structured error model.
package problem

import "fmt"

// ProblemCode is a stable string identifier for a class of problem.
// Codes are prefixed by category to aid routing and observability:
//   - VAL_ : input validation and argument errors
//   - SYS_ : system-level / cross-cutting errors
//   - MD_  : marketdata ingestion errors
//   - AGG_ : aggregation / order-book errors
//   - DEL_ : delivery / session errors
//
//nolint:revive // ProblemCode is explicit and stable across packages.
type ProblemCode string

const (
	// ValidationFailed indicates generic input validation failure.
	ValidationFailed ProblemCode = "VAL_VALIDATION_FAILED"
	// InvalidArgument indicates invalid argument shape/value.
	InvalidArgument ProblemCode = "VAL_INVALID_ARGUMENT"

	// NotFound indicates requested resource was not found.
	NotFound ProblemCode = "SYS_NOT_FOUND"
	// Conflict indicates conflict with current system state.
	Conflict ProblemCode = "SYS_CONFLICT"
	// Internal indicates unexpected system/internal error.
	Internal ProblemCode = "SYS_INTERNAL"

	// OutOfOrder indicates sequence/ordering violation.
	OutOfOrder ProblemCode = "MD_OUT_OF_ORDER"
	// Duplicate indicates duplicate event/message.
	Duplicate ProblemCode = "MD_DUPLICATE"

	// IntegrityViolation indicates a broken domain invariant.
	IntegrityViolation ProblemCode = "AGG_INTEGRITY_VIOLATION"
)

// Problem is the canonical error type for the system.
// It is used in place of plain errors to carry structured context.
type Problem struct {
	Code      ProblemCode
	Message   string
	Details   map[string]any
	Cause     error
	Retryable bool
}

// Error implements the error interface so Problem can interop with stdlib.
func (p *Problem) Error() string {
	if p.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", p.Code, p.Message, p.Cause)
	}
	return fmt.Sprintf("%s: %s", p.Code, p.Message)
}

// Unwrap allows errors.Is / errors.As traversal.
func (p *Problem) Unwrap() error { return p.Cause }

// New creates a Problem with code and message.
func New(code ProblemCode, msg string) *Problem {
	return &Problem{Code: code, Message: msg, Details: make(map[string]any)}
}

// Newf creates a Problem with a formatted message.
func Newf(code ProblemCode, format string, args ...any) *Problem {
	return New(code, fmt.Sprintf(format, args...))
}

// WithDetail returns a copy of p with an additional detail entry.
// It is safe to call on nil (returns nil).
func WithDetail(p *Problem, key string, value any) *Problem {
	if p == nil {
		return nil
	}
	out := *p
	out.Details = make(map[string]any, len(p.Details)+1)
	for k, v := range p.Details {
		out.Details[k] = v
	}
	out.Details[key] = value
	return &out
}

// WithRetryable marks the problem as retryable.
func WithRetryable(p *Problem) *Problem {
	if p == nil {
		return nil
	}
	out := *p
	out.Retryable = true
	return &out
}

// Wrap wraps an existing error as the cause of a new Problem.
func Wrap(err error, code ProblemCode, msg string) *Problem {
	p := New(code, msg)
	p.Cause = err
	return p
}
