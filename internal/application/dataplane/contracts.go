package dataplane

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

const (
	EventTypeMessage          = "dataplane.message"
	EventTypeValidationResult = "dataplane.validation_result"
	DefaultStateBucket        = "MR_DATAPLANE"
	DefaultResultsLimit       = 100
	defaultEnvelopeVenue      = "binding"
)

type RuleKind string

const (
	RuleRequired RuleKind = "required"
	RuleNotEmpty RuleKind = "not_empty"
	RuleEquals   RuleKind = "equals"
)

type ResultStatus string

const (
	ResultStatusPassed ResultStatus = "passed"
	ResultStatusFailed ResultStatus = "failed"
	ResultStatusError  ResultStatus = "error"
)

type Binding struct {
	Name       string `json:"name"`
	KafkaTopic string `json:"kafka_topic"`
}

func (b Binding) Validate() *problem.Problem {
	if strings.TrimSpace(b.Name) == "" {
		return problem.New(problem.ValidationFailed, "binding.name must not be empty")
	}
	if strings.TrimSpace(b.KafkaTopic) == "" {
		return problem.New(problem.ValidationFailed, "binding.kafka_topic must not be empty")
	}
	return nil
}

type Rule struct {
	ID       string   `json:"id,omitempty"`
	Field    string   `json:"field"`
	Kind     RuleKind `json:"kind"`
	Expected string   `json:"expected,omitempty"`
}

func (r Rule) EffectiveID() string {
	if strings.TrimSpace(r.ID) != "" {
		return strings.TrimSpace(r.ID)
	}
	return fmt.Sprintf("%s:%s", r.Kind, strings.TrimSpace(r.Field))
}

func (r Rule) Validate() *problem.Problem {
	if strings.TrimSpace(r.Field) == "" {
		return problem.New(problem.ValidationFailed, "rule.field must not be empty")
	}
	switch r.Kind {
	case RuleRequired, RuleNotEmpty:
		return nil
	case RuleEquals:
		if strings.TrimSpace(r.Expected) == "" {
			return problem.New(problem.ValidationFailed, "rule.expected must not be empty when kind=equals")
		}
		return nil
	default:
		return problem.Newf(problem.ValidationFailed, "unsupported rule.kind %q", r.Kind)
	}
}

type Config struct {
	Name    string `json:"name"`
	Binding string `json:"binding"`
	Version string `json:"version"`
	Rules   []Rule `json:"rules"`
}

func (c Config) Validate() *problem.Problem {
	if strings.TrimSpace(c.Name) == "" {
		return problem.New(problem.ValidationFailed, "config.name must not be empty")
	}
	if strings.TrimSpace(c.Binding) == "" {
		return problem.New(problem.ValidationFailed, "config.binding must not be empty")
	}
	if strings.TrimSpace(c.Version) == "" {
		return problem.New(problem.ValidationFailed, "config.version must not be empty")
	}
	if len(c.Rules) == 0 {
		return problem.New(problem.ValidationFailed, "config.rules must not be empty")
	}
	for i, rule := range c.Rules {
		if p := rule.Validate(); p != nil {
			return problem.WithDetail(p, "rule_index", i)
		}
	}
	return nil
}

type Activation struct {
	Binding     string `json:"binding"`
	Version     string `json:"version"`
	ActivatedAt int64  `json:"activated_at"`
}

func (a Activation) Validate() *problem.Problem {
	if strings.TrimSpace(a.Binding) == "" {
		return problem.New(problem.ValidationFailed, "activation.binding must not be empty")
	}
	if strings.TrimSpace(a.Version) == "" {
		return problem.New(problem.ValidationFailed, "activation.version must not be empty")
	}
	if a.ActivatedAt <= 0 {
		return problem.New(problem.ValidationFailed, "activation.activated_at must be positive")
	}
	return nil
}

type Message struct {
	MessageID     string          `json:"message_id"`
	Binding       string          `json:"binding"`
	Topic         string          `json:"topic"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	ProducedAt    int64           `json:"produced_at"`
	Payload       json.RawMessage `json:"payload"`
}

func (m Message) Validate() *problem.Problem {
	if strings.TrimSpace(m.MessageID) == "" {
		return problem.New(problem.ValidationFailed, "message.message_id must not be empty")
	}
	if strings.TrimSpace(m.Binding) == "" {
		return problem.New(problem.ValidationFailed, "message.binding must not be empty")
	}
	if strings.TrimSpace(m.Topic) == "" {
		return problem.New(problem.ValidationFailed, "message.topic must not be empty")
	}
	if m.ProducedAt <= 0 {
		return problem.New(problem.ValidationFailed, "message.produced_at must be positive")
	}
	if _, p := DecodePayloadObject(m.Payload); p != nil {
		return p
	}
	return nil
}

type Violation struct {
	Rule     string `json:"rule"`
	Field    string `json:"field"`
	Reason   string `json:"reason"`
	Expected string `json:"expected,omitempty"`
}

type ValidationResult struct {
	MessageID     string       `json:"message_id"`
	Binding       string       `json:"binding"`
	Topic         string       `json:"topic"`
	CorrelationID string       `json:"correlation_id,omitempty"`
	ConfigName    string       `json:"config_name,omitempty"`
	ConfigVersion string       `json:"config_version,omitempty"`
	Status        ResultStatus `json:"status"`
	Violations    []Violation  `json:"violations,omitempty"`
	ProcessedAt   int64        `json:"processed_at"`
}

func (r ValidationResult) Validate() *problem.Problem {
	if strings.TrimSpace(r.MessageID) == "" {
		return problem.New(problem.ValidationFailed, "result.message_id must not be empty")
	}
	if strings.TrimSpace(r.Binding) == "" {
		return problem.New(problem.ValidationFailed, "result.binding must not be empty")
	}
	if strings.TrimSpace(r.Topic) == "" {
		return problem.New(problem.ValidationFailed, "result.topic must not be empty")
	}
	if r.ProcessedAt <= 0 {
		return problem.New(problem.ValidationFailed, "result.processed_at must be positive")
	}
	switch r.Status {
	case ResultStatusPassed, ResultStatusFailed, ResultStatusError:
	default:
		return problem.Newf(problem.ValidationFailed, "unsupported result.status %q", r.Status)
	}
	return nil
}

func DecodePayloadObject(raw []byte) (map[string]any, *problem.Problem) {
	if len(raw) == 0 {
		return nil, problem.New(problem.ValidationFailed, "message.payload must not be empty")
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, problem.Wrap(err, problem.ValidationFailed, "message.payload must be valid JSON object")
	}
	if payload == nil {
		return nil, problem.New(problem.ValidationFailed, "message.payload must decode to JSON object")
	}
	return payload, nil
}

func NewMessageEnvelope(msg Message) (envelope.Envelope, *problem.Problem) {
	if p := msg.Validate(); p != nil {
		return envelope.Envelope{}, p
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return envelope.Envelope{}, problem.Wrap(err, problem.Internal, "marshal dataplane message failed")
	}
	return envelope.Envelope{
		Type:           EventTypeMessage,
		Version:        1,
		Venue:          defaultEnvelopeVenue,
		Instrument:     msg.Binding,
		TsExchange:     msg.ProducedAt,
		TsIngest:       msg.ProducedAt,
		Seq:            0,
		IdempotencyKey: msg.MessageID,
		ContentType:    envelope.ContentTypeJSON,
		Meta: map[string]string{
			"binding":    msg.Binding,
			"topic":      msg.Topic,
			"message_id": msg.MessageID,
		},
		Payload: payload,
	}, nil
}

func MessageFromEnvelope(env envelope.Envelope) (Message, *problem.Problem) {
	if strings.TrimSpace(env.Type) != EventTypeMessage {
		return Message{}, problem.Newf(problem.ValidationFailed, "unexpected envelope type %q", env.Type)
	}
	var msg Message
	if err := json.Unmarshal(env.Payload, &msg); err != nil {
		return Message{}, problem.Wrap(err, problem.ValidationFailed, "decode dataplane message failed")
	}
	return msg, msg.Validate()
}

func NewValidationResultEnvelope(result ValidationResult) (envelope.Envelope, *problem.Problem) {
	if p := result.Validate(); p != nil {
		return envelope.Envelope{}, p
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return envelope.Envelope{}, problem.Wrap(err, problem.Internal, "marshal validation result failed")
	}
	return envelope.Envelope{
		Type:           EventTypeValidationResult,
		Version:        1,
		Venue:          defaultEnvelopeVenue,
		Instrument:     result.Binding,
		TsExchange:     result.ProcessedAt,
		TsIngest:       result.ProcessedAt,
		Seq:            0,
		IdempotencyKey: fmt.Sprintf("%s:%s", result.MessageID, result.ConfigVersion),
		ContentType:    envelope.ContentTypeJSON,
		Meta: map[string]string{
			"binding":    result.Binding,
			"topic":      result.Topic,
			"message_id": result.MessageID,
		},
		Payload: payload,
	}, nil
}

func ValidationResultFromEnvelope(env envelope.Envelope) (ValidationResult, *problem.Problem) {
	if strings.TrimSpace(env.Type) != EventTypeValidationResult {
		return ValidationResult{}, problem.Newf(problem.ValidationFailed, "unexpected envelope type %q", env.Type)
	}
	var result ValidationResult
	if err := json.Unmarshal(env.Payload, &result); err != nil {
		return ValidationResult{}, problem.Wrap(err, problem.ValidationFailed, "decode validation result failed")
	}
	return result, result.Validate()
}

type ResultQuery struct {
	Binding       string
	MessageID     string
	CorrelationID string
	Limit         int
}

type ResultStore interface {
	Save(result ValidationResult) *problem.Problem
	List(ctx context.Context, query ResultQuery) ([]ValidationResult, *problem.Problem)
	Get(ctx context.Context, messageID string) (ValidationResult, *problem.Problem)
}

type MemoryResultStore struct {
	mu       sync.RWMutex
	capacity int
	order    []string
	results  map[string]ValidationResult
}

func NewMemoryResultStore(capacity int) *MemoryResultStore {
	if capacity <= 0 {
		capacity = DefaultResultsLimit
	}
	return &MemoryResultStore{
		capacity: capacity,
		order:    make([]string, 0, capacity),
		results:  make(map[string]ValidationResult, capacity),
	}
}

func (s *MemoryResultStore) Save(result ValidationResult) *problem.Problem {
	if s == nil {
		return problem.New(problem.Internal, "result store must not be nil")
	}
	if p := result.Validate(); p != nil {
		return p
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.results[result.MessageID]; !exists {
		s.order = append(s.order, result.MessageID)
	}
	s.results[result.MessageID] = result
	for len(s.order) > s.capacity {
		evictID := s.order[0]
		s.order = s.order[1:]
		delete(s.results, evictID)
	}
	return nil
}

func (s *MemoryResultStore) Get(_ context.Context, messageID string) (ValidationResult, *problem.Problem) {
	if s == nil {
		return ValidationResult{}, problem.New(problem.Internal, "result store must not be nil")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return ValidationResult{}, problem.New(problem.ValidationFailed, "result query message_id must not be empty")
	}
	result, ok := s.results[messageID]
	if !ok {
		return ValidationResult{}, problem.Newf(problem.NotFound, "validation result %q not found", messageID)
	}
	return result, nil
}

func (s *MemoryResultStore) List(_ context.Context, query ResultQuery) ([]ValidationResult, *problem.Problem) {
	if s == nil {
		return nil, problem.New(problem.Internal, "result store must not be nil")
	}
	limit := query.Limit
	if limit <= 0 {
		limit = DefaultResultsLimit
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]ValidationResult, 0, limit)
	for i := len(s.order) - 1; i >= 0; i-- {
		result, ok := s.results[s.order[i]]
		if !ok {
			continue
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
		if len(results) == limit {
			break
		}
	}
	return results, nil
}
