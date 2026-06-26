package validatorruntime

import (
	"context"
	"fmt"
	"strings"

	"github.com/FabioCaffarello/stream-analytics/internal/application/dataplane"
	"github.com/FabioCaffarello/stream-analytics/internal/application/runtimebootstrap"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

type Clock interface {
	NowUnixMilli() int64
}

type Publisher interface {
	Publish(ctx context.Context, env envelope.Envelope) *problem.Problem
}

type Processor struct {
	runtime     *runtimebootstrap.Runtime
	clock       Clock
	publisher   Publisher
	resultStore dataplane.ResultStore
}

func NewProcessor(runtime *runtimebootstrap.Runtime, clock Clock, publisher Publisher, resultStore dataplane.ResultStore) *Processor {
	return &Processor{runtime: runtime, clock: clock, publisher: publisher, resultStore: resultStore}
}

func (p *Processor) Process(ctx context.Context, msg dataplane.Message) (dataplane.ValidationResult, *problem.Problem) {
	if p == nil || p.runtime == nil || p.clock == nil || p.publisher == nil || p.resultStore == nil {
		return dataplane.ValidationResult{}, problem.New(problem.Internal, "validator processor dependencies must not be nil")
	}
	if prob := msg.Validate(); prob != nil {
		return dataplane.ValidationResult{}, prob
	}

	activeCfg, prob := p.runtime.ActiveConfig(ctx, msg.Binding)
	if prob != nil {
		result := dataplane.ValidationResult{
			MessageID:     msg.MessageID,
			Binding:       msg.Binding,
			Topic:         msg.Topic,
			CorrelationID: msg.CorrelationID,
			Status:        dataplane.ResultStatusError,
			Violations: []dataplane.Violation{{
				Rule:   "runtime.active_config",
				Field:  "",
				Reason: "active_config_not_found",
			}},
			ProcessedAt: p.clock.NowUnixMilli(),
		}
		return result, p.publishResult(ctx, result)
	}

	payload, prob := dataplane.DecodePayloadObject(msg.Payload)
	if prob != nil {
		result := dataplane.ValidationResult{
			MessageID:     msg.MessageID,
			Binding:       msg.Binding,
			Topic:         msg.Topic,
			CorrelationID: msg.CorrelationID,
			ConfigName:    activeCfg.Name,
			ConfigVersion: activeCfg.Version,
			Status:        dataplane.ResultStatusError,
			Violations: []dataplane.Violation{{
				Rule:   "payload",
				Reason: "payload_decode_failed",
			}},
			ProcessedAt: p.clock.NowUnixMilli(),
		}
		return result, p.publishResult(ctx, result)
	}

	violations := evaluateRules(payload, activeCfg.Rules)
	status := dataplane.ResultStatusPassed
	if len(violations) > 0 {
		status = dataplane.ResultStatusFailed
	}
	result := dataplane.ValidationResult{
		MessageID:     msg.MessageID,
		Binding:       msg.Binding,
		Topic:         msg.Topic,
		CorrelationID: msg.CorrelationID,
		ConfigName:    activeCfg.Name,
		ConfigVersion: activeCfg.Version,
		Status:        status,
		Violations:    violations,
		ProcessedAt:   p.clock.NowUnixMilli(),
	}
	return result, p.publishResult(ctx, result)
}

func (p *Processor) publishResult(ctx context.Context, result dataplane.ValidationResult) *problem.Problem {
	if prob := p.resultStore.Save(result); prob != nil {
		return prob
	}
	env, prob := dataplane.NewValidationResultEnvelope(result)
	if prob != nil {
		return prob
	}
	return p.publisher.Publish(ctx, env)
}

func evaluateRules(payload map[string]any, rules []dataplane.Rule) []dataplane.Violation {
	violations := make([]dataplane.Violation, 0, len(rules))
	for _, rule := range rules {
		value, exists := payload[rule.Field]
		switch rule.Kind {
		case dataplane.RuleRequired:
			if !exists {
				violations = append(violations, dataplane.Violation{
					Rule:   rule.EffectiveID(),
					Field:  rule.Field,
					Reason: "missing_required_field",
				})
			}
		case dataplane.RuleNotEmpty:
			if !exists || isEmptyValue(value) {
				violations = append(violations, dataplane.Violation{
					Rule:   rule.EffectiveID(),
					Field:  rule.Field,
					Reason: "field_must_not_be_empty",
				})
			}
		case dataplane.RuleEquals:
			if !exists || normalizeValue(value) != rule.Expected {
				violations = append(violations, dataplane.Violation{
					Rule:     rule.EffectiveID(),
					Field:    rule.Field,
					Reason:   "field_must_equal_expected_value",
					Expected: rule.Expected,
				})
			}
		}
	}
	return violations
}

func isEmptyValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(v) == ""
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	default:
		return false
	}
}

func normalizeValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}
