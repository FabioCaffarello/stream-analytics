package timescale

import (
	"context"
	"sort"
	"sync"

	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/core/delivery/ports"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

// DeliveryRangeStore is an in-memory hot-path store used by delivery getrange.
// TODO(m2): replace with real Timescale reads.
type DeliveryRangeStore struct {
	mu            sync.RWMutex
	maxPerSubject int
	bySubject     map[string][]ports.RangeItem
}

func NewDeliveryRangeStore(maxPerSubject int) *DeliveryRangeStore {
	if maxPerSubject <= 0 {
		maxPerSubject = 4096
	}
	return &DeliveryRangeStore{
		maxPerSubject: maxPerSubject,
		bySubject:     make(map[string][]ports.RangeItem),
	}
}

func (s *DeliveryRangeStore) StoreEnvelope(env envelope.Envelope) {
	sub, p := domain.SubjectFromEnvelope(env, domain.DefaultTimeframe)
	if p != nil {
		return
	}
	key := sub.String()
	item := ports.RangeItem{
		Seq:      env.Seq,
		TsIngest: env.TsIngest,
		Payload:  append([]byte(nil), env.Payload...),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	items := append(s.bySubject[key], item)
	if len(items) > s.maxPerSubject {
		items = items[len(items)-s.maxPerSubject:]
	}
	s.bySubject[key] = items
}

func (s *DeliveryRangeStore) GetRange(_ context.Context, subject domain.Subject, fromMs, toMs int64, limit int) ([]ports.RangeItem, *problem.Problem) {
	s.mu.RLock()
	items := append([]ports.RangeItem(nil), s.bySubject[subject.String()]...)
	s.mu.RUnlock()

	if len(items) == 0 {
		return nil, nil
	}
	filtered := make([]ports.RangeItem, 0, len(items))
	for _, it := range items {
		if fromMs > 0 && it.TsIngest < fromMs {
			continue
		}
		if toMs > 0 && it.TsIngest > toMs {
			continue
		}
		filtered = append(filtered, it)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].TsIngest == filtered[j].TsIngest {
			return filtered[i].Seq < filtered[j].Seq
		}
		return filtered[i].TsIngest < filtered[j].TsIngest
	})
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	return filtered, nil
}
