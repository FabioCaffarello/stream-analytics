package timescale_test

import (
	"errors"

	"github.com/market-raccoon/internal/shared/problem"
)

func testUnavailableProblem() *problem.Problem {
	return problem.Wrap(errors.New("db down"), problem.Unavailable, "timescale exec failed")
}
