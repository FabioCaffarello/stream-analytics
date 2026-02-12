// Package clock provides abstractions for deterministic time handling.
package clock

import "time"

// Clock abstracts time access so domain code stays deterministic in tests.
type Clock interface {
	// Now returns the current time.
	Now() time.Time
	// NowUnixMilli returns current time as Unix milliseconds.
	NowUnixMilli() int64
}
