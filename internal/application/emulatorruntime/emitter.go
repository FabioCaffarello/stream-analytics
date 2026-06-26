package emulatorruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/application/dataplane"
	"github.com/FabioCaffarello/stream-analytics/internal/application/runtimebootstrap"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

const (
	ScenarioValid           = "valid"
	ScenarioMissingRequired = "missing_required"
)

type Clock interface {
	NowUnixMilli() int64
}

type Writer interface {
	Write(ctx context.Context, topic string, key, value []byte, headers map[string]string) *problem.Problem
}

type Emitter struct {
	runtime *runtimebootstrap.Runtime
	writer  Writer
	clock   Clock
}

func NewEmitter(runtime *runtimebootstrap.Runtime, writer Writer, clock Clock) *Emitter {
	return &Emitter{
		runtime: runtime,
		writer:  writer,
		clock:   clock,
	}
}

func (e *Emitter) Emit(ctx context.Context, bindingName, scenario string) (dataplane.Message, *problem.Problem) {
	if e == nil || e.runtime == nil || e.writer == nil || e.clock == nil {
		return dataplane.Message{}, problem.New(problem.Internal, "emitter dependencies must not be nil")
	}
	binding, p := e.runtime.BindingByName(ctx, bindingName)
	if p != nil {
		return dataplane.Message{}, p
	}
	payload, p := payloadForScenario(scenario)
	if p != nil {
		return dataplane.Message{}, p
	}
	producedAt := e.clock.NowUnixMilli()
	messageID := fmt.Sprintf("%s-%d", binding.Name, time.Now().UnixNano())
	correlationID := fmt.Sprintf("corr-%s-%d", binding.Name, producedAt)
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return dataplane.Message{}, problem.Wrap(err, problem.Internal, "marshal emulator payload failed")
	}
	if p := e.writer.Write(ctx, binding.KafkaTopic, []byte(messageID), rawPayload, map[string]string{
		"message_id":     messageID,
		"correlation_id": correlationID,
	}); p != nil {
		return dataplane.Message{}, p
	}
	return dataplane.Message{
		MessageID:     messageID,
		Binding:       binding.Name,
		Topic:         binding.KafkaTopic,
		CorrelationID: correlationID,
		ProducedAt:    producedAt,
		Payload:       rawPayload,
	}, nil
}

func payloadForScenario(scenario string) (map[string]any, *problem.Problem) {
	switch strings.ToLower(strings.TrimSpace(scenario)) {
	case "", ScenarioValid:
		return map[string]any{
			"account_id": "acct-100",
			"status":     "OPEN",
		}, nil
	case ScenarioMissingRequired:
		return map[string]any{
			"status": "OPEN",
		}, nil
	default:
		return nil, problem.Newf(problem.ValidationFailed, "unsupported emulator scenario %q", scenario)
	}
}
