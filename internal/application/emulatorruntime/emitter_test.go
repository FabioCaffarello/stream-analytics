package emulatorruntime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/application/dataplane"
	"github.com/FabioCaffarello/stream-analytics/internal/application/runtimebootstrap"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

type fakeClock struct{ now int64 }

func (f fakeClock) NowUnixMilli() int64 { return f.now }

type fakeWriter struct {
	topic   string
	headers map[string]string
	value   []byte
}

func (w *fakeWriter) Write(_ context.Context, topic string, _ []byte, value []byte, headers map[string]string) *problem.Problem {
	w.topic = topic
	w.value = append([]byte(nil), value...)
	w.headers = headers
	return nil
}

type fakeStore struct{}

func (fakeStore) UpsertBinding(context.Context, dataplane.Binding) *problem.Problem { return nil }
func (fakeStore) ListBindings(context.Context) ([]dataplane.Binding, *problem.Problem) {
	return []dataplane.Binding{{Name: "orders", KafkaTopic: "mr.orders"}}, nil
}
func (fakeStore) UpsertConfig(context.Context, dataplane.Config) *problem.Problem { return nil }
func (fakeStore) ListConfigs(context.Context, string) ([]dataplane.Config, *problem.Problem) {
	return nil, nil
}
func (fakeStore) ActivateConfig(context.Context, dataplane.Activation) *problem.Problem { return nil }
func (fakeStore) ActiveConfig(context.Context, string) (*dataplane.Config, *problem.Problem) {
	return nil, problem.New(problem.NotFound, "not used")
}

func TestEmitterEmitMissingRequiredScenario(t *testing.T) {
	writer := &fakeWriter{}
	emitter := NewEmitter(runtimebootstrap.New(fakeStore{}), writer, fakeClock{now: 10})
	msg, p := emitter.Emit(context.Background(), "orders", ScenarioMissingRequired)
	if p != nil {
		t.Fatalf("emit: %v", p)
	}
	if msg.Binding != "orders" {
		t.Fatalf("binding=%q want=orders", msg.Binding)
	}
	if writer.topic != "mr.orders" {
		t.Fatalf("writer topic=%q want=mr.orders", writer.topic)
	}
	var payload map[string]any
	if err := json.Unmarshal(writer.value, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if _, ok := payload["account_id"]; ok {
		t.Fatal("missing_required scenario should omit account_id")
	}
}
