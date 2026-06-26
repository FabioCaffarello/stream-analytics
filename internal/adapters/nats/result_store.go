package nats

import (
	"context"
	"sort"
	"strings"

	"github.com/FabioCaffarello/stream-analytics/internal/application/dataplane"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
	"github.com/nats-io/nats.go"
)

const resultKeyPrefix = "results."

type ResultStore struct {
	nc               *nats.Conn
	kv               nats.KeyValue
	defaultListLimit int
}

var _ dataplane.ResultStore = (*ResultStore)(nil)

func NewResultStore(_ context.Context, url, bucket string, defaultListLimit int) (*ResultStore, *problem.Problem) {
	nc, kv, p := openKeyValueBucket(url, bucket, "stream-analytics-dataplane-results")
	if p != nil {
		return nil, p
	}
	if defaultListLimit <= 0 {
		defaultListLimit = dataplane.DefaultResultsLimit
	}
	return &ResultStore{nc: nc, kv: kv, defaultListLimit: defaultListLimit}, nil
}

func (s *ResultStore) Close() error {
	if s == nil || s.nc == nil {
		return nil
	}
	s.nc.Drain()
	s.nc.Close()
	return nil
}

func (s *ResultStore) Save(result dataplane.ValidationResult) *problem.Problem {
	if s == nil || s.kv == nil {
		return problem.New(problem.Internal, "validation result store must not be nil")
	}
	if p := result.Validate(); p != nil {
		return p
	}
	return kvPutJSON(s.kv, resultKey(result.MessageID), result)
}

func (s *ResultStore) Get(_ context.Context, messageID string) (dataplane.ValidationResult, *problem.Problem) {
	if s == nil || s.kv == nil {
		return dataplane.ValidationResult{}, problem.New(problem.Internal, "validation result store must not be nil")
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return dataplane.ValidationResult{}, problem.New(problem.ValidationFailed, "result query message_id must not be empty")
	}
	var result dataplane.ValidationResult
	if p := kvGetJSON(s.kv, resultKey(messageID), &result); p != nil {
		return dataplane.ValidationResult{}, p
	}
	if p := result.Validate(); p != nil {
		return dataplane.ValidationResult{}, p
	}
	return result, nil
}

//nolint:gocyclo // Filter/sort/pagination branches are inherently enumerated here; no meaningful decomposition.
func (s *ResultStore) List(_ context.Context, query dataplane.ResultQuery) ([]dataplane.ValidationResult, *problem.Problem) {
	if s == nil || s.kv == nil {
		return nil, problem.New(problem.Internal, "validation result store must not be nil")
	}
	keys, p := kvKeysWithPrefix(s.kv, resultKeyPrefix)
	if p != nil {
		return nil, p
	}
	results := make([]dataplane.ValidationResult, 0, len(keys))
	for _, key := range keys {
		var result dataplane.ValidationResult
		if p := kvGetJSON(s.kv, key, &result); p != nil {
			return nil, p
		}
		if p := result.Validate(); p != nil {
			return nil, p
		}
		if query.MessageID != "" && result.MessageID != query.MessageID {
			continue
		}
		if query.Binding != "" && result.Binding != query.Binding {
			continue
		}
		if query.CorrelationID != "" && result.CorrelationID != query.CorrelationID {
			continue
		}
		results = append(results, result)
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].ProcessedAt == results[j].ProcessedAt {
			return results[i].MessageID > results[j].MessageID
		}
		return results[i].ProcessedAt > results[j].ProcessedAt
	})
	limit := query.Limit
	if limit <= 0 {
		limit = s.defaultListLimit
	}
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func resultKey(messageID string) string {
	return resultKeyPrefix + sanitize(messageID)
}
