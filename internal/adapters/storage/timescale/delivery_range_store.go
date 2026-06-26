package timescale

import (
	"context"
	"log/slog"
	"sort"
	"sync"

	"github.com/FabioCaffarello/stream-analytics/internal/core/delivery/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/core/delivery/ports"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DeliveryRangeStore is an in-memory hot-path store used by delivery getrange.
// For Timescale-backed reads, use PgRangeStore (wired when storage.timescale.enabled=true).
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
	tf := domain.TimeframeFromEnvelopeMeta(env, domain.DefaultTimeframe)
	sub, p := domain.SubjectFromEnvelope(env, tf)
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

type deliveryPGQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// PgRangeStore is a Timescale-backed range store used when storage is enabled.
type PgRangeStore struct {
	pool          deliveryPGQuerier
	maxPerSubject int
}

func NewPgRangeStore(pool *Pool, maxPerSubject int) *PgRangeStore {
	if pool == nil || pool.Raw() == nil {
		return &PgRangeStore{maxPerSubject: maxPerSubject}
	}
	return NewPgRangeStoreWithQuerier(pool.Raw(), maxPerSubject)
}

func NewPgRangeStoreWithQuerier(pool deliveryPGQuerier, maxPerSubject int) *PgRangeStore {
	if maxPerSubject <= 0 {
		maxPerSubject = 4096
	}
	return &PgRangeStore{
		pool:          pool,
		maxPerSubject: maxPerSubject,
	}
}

func (s *PgRangeStore) StoreEnvelope(env envelope.Envelope) {
	if s == nil || s.pool == nil {
		return
	}
	tf := domain.TimeframeFromEnvelopeMeta(env, domain.DefaultTimeframe)
	sub, p := domain.SubjectFromEnvelope(env, tf)
	if p != nil {
		return
	}
	const insertSQL = `
INSERT INTO delivery_events (subject, seq, ts_ingest, payload, created_at)
VALUES ($1, $2, $3, $4, NOW())
ON CONFLICT (subject, seq) DO UPDATE
SET ts_ingest = EXCLUDED.ts_ingest,
    payload = EXCLUDED.payload`
	if _, err := s.pool.Exec(context.Background(), insertSQL, sub.String(), env.Seq, env.TsIngest, env.Payload); err != nil {
		slog.Warn("delivery range store: insert failed", "subject", sub.String(), "seq", env.Seq, "err", err)
	}
}

func (s *PgRangeStore) GetRange(ctx context.Context, subject domain.Subject, fromMs, toMs int64, limit int) ([]ports.RangeItem, *problem.Problem) {
	if s == nil || s.pool == nil {
		return nil, nil
	}
	if limit <= 0 || limit > s.maxPerSubject {
		limit = s.maxPerSubject
	}

	const querySQL = `
SELECT seq, ts_ingest, payload
FROM delivery_events
WHERE subject = $1
  AND ($2 = 0 OR ts_ingest >= $2)
  AND ($3 = 0 OR ts_ingest <= $3)
ORDER BY ts_ingest ASC, seq ASC
LIMIT $4`

	rows, err := s.pool.Query(ctx, querySQL, subject.String(), fromMs, toMs, limit)
	if err != nil {
		// Graceful degradation: return empty range when backend is unavailable.
		return nil, nil
	}
	defer rows.Close()

	items := make([]ports.RangeItem, 0, limit)
	for rows.Next() {
		var item ports.RangeItem
		if err := rows.Scan(&item.Seq, &item.TsIngest, &item.Payload); err != nil {
			return nil, problem.Wrap(err, problem.Internal, "timescale range scan failed")
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "timescale range rows failed")
	}
	return items, nil
}
