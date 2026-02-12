package app

import (
	"context"

	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/core/delivery/ports"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/result"
)

// GetRangeRequest is a historical data query from one client session.
type GetRangeRequest struct {
	SubjectRaw string
	FromMs     int64
	ToMs       int64
	Limit      int
}

type SessionService struct {
	rangeStore ports.RangeStore
}

func NewSessionService(rangeStore ports.RangeStore) *SessionService {
	return &SessionService{rangeStore: rangeStore}
}

func (s *SessionService) ParseSubject(raw string) result.Result[domain.Subject] {
	subject, p := domain.ParseSubject(raw)
	if p != nil {
		return result.FailProblem[domain.Subject](p)
	}
	return result.Ok(subject)
}

func (s *SessionService) GetRange(ctx context.Context, req GetRangeRequest) result.Result[[]ports.RangeItem] {
	if req.Limit < 0 {
		return result.FailProblem[[]ports.RangeItem](problem.New(problem.ValidationFailed, "limit must be >= 0"))
	}
	subject, p := domain.ParseSubject(req.SubjectRaw)
	if p != nil {
		return result.FailProblem[[]ports.RangeItem](p)
	}
	if s.rangeStore == nil {
		return result.FailProblem[[]ports.RangeItem](problem.New(problem.NotFound, "range store unavailable"))
	}
	items, p := s.rangeStore.GetRange(ctx, subject, req.FromMs, req.ToMs, req.Limit)
	if p != nil {
		return result.FailProblem[[]ports.RangeItem](p)
	}
	return result.Ok(items)
}
