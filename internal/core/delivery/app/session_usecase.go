package app

import (
	"context"
	"strings"

	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/core/delivery/ports"
	"github.com/market-raccoon/internal/shared/metrics"
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
	// WS clients may query market-type aliases (e.g. BTCUSDT:SPOT) while
	// persisted delivery history is keyed by the canonical symbol (BTCUSDT).
	// If the direct lookup misses, retry once with market-type suffix stripped.
	if len(items) == 0 {
		if alias, ok := subjectWithoutMarketTypeSuffix(subject); ok {
			aliasItems, aliasP := s.rangeStore.GetRange(ctx, alias, req.FromMs, req.ToMs, req.Limit)
			if aliasP != nil {
				metrics.IncDeliveryRangeAliasFallback("error")
				return result.FailProblem[[]ports.RangeItem](aliasP)
			}
			if len(aliasItems) > 0 {
				metrics.IncDeliveryRangeAliasFallback("hit")
			} else {
				metrics.IncDeliveryRangeAliasFallback("miss")
			}
			items = aliasItems
		}
	}
	return result.Ok(items)
}

func subjectWithoutMarketTypeSuffix(subject domain.Subject) (domain.Subject, bool) {
	symbol := subject.Symbol
	colon := strings.Index(symbol, ":")
	if colon < 0 {
		return domain.Subject{}, false
	}
	base := domain.NormalizeSymbol(symbol[:colon])
	if base == "" || base == symbol {
		return domain.Subject{}, false
	}
	alias := subject
	alias.Symbol = base
	return alias, true
}
