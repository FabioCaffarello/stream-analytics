package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/market-raccoon/internal/application/dataplane"
	"github.com/market-raccoon/internal/application/runtimebootstrap"
	"github.com/market-raccoon/internal/shared/problem"
)

type dataplaneStore struct {
	bindings map[string]dataplane.Binding
	configs  map[string]map[string]dataplane.Config
	active   map[string]string
}

func newDataplaneStore() *dataplaneStore {
	return &dataplaneStore{
		bindings: make(map[string]dataplane.Binding),
		configs:  make(map[string]map[string]dataplane.Config),
		active:   make(map[string]string),
	}
}

func (s *dataplaneStore) UpsertBinding(_ context.Context, binding dataplane.Binding) *problem.Problem {
	s.bindings[binding.Name] = binding
	return nil
}

func (s *dataplaneStore) ListBindings(_ context.Context) ([]dataplane.Binding, *problem.Problem) {
	out := make([]dataplane.Binding, 0, len(s.bindings))
	for _, binding := range s.bindings {
		out = append(out, binding)
	}
	return out, nil
}

func (s *dataplaneStore) UpsertConfig(_ context.Context, cfg dataplane.Config) *problem.Problem {
	if _, ok := s.configs[cfg.Binding]; !ok {
		s.configs[cfg.Binding] = make(map[string]dataplane.Config)
	}
	s.configs[cfg.Binding][cfg.Version] = cfg
	return nil
}

func (s *dataplaneStore) ListConfigs(_ context.Context, binding string) ([]dataplane.Config, *problem.Problem) {
	if binding == "" {
		var out []dataplane.Config
		for _, byVersion := range s.configs {
			for _, cfg := range byVersion {
				out = append(out, cfg)
			}
		}
		return out, nil
	}
	byVersion := s.configs[binding]
	out := make([]dataplane.Config, 0, len(byVersion))
	for _, cfg := range byVersion {
		out = append(out, cfg)
	}
	return out, nil
}

func (s *dataplaneStore) ActivateConfig(_ context.Context, activation dataplane.Activation) *problem.Problem {
	s.active[activation.Binding] = activation.Version
	return nil
}

func (s *dataplaneStore) ActiveConfig(_ context.Context, binding string) (*dataplane.Config, *problem.Problem) {
	version, ok := s.active[binding]
	if !ok {
		return nil, problem.New(problem.NotFound, "active config not found")
	}
	cfg := s.configs[binding][version]
	return &cfg, nil
}

type fakeEmitter struct {
	message dataplane.Message
}

func (e fakeEmitter) Emit(context.Context, string, string) (dataplane.Message, *problem.Problem) {
	return e.message, nil
}

func TestDataPlaneHandlers_UpsertBindingAndListResults(t *testing.T) {
	store := newDataplaneStore()
	runtime := runtimebootstrap.New(store)
	results := dataplane.NewMemoryResultStore(10)
	if p := results.Save(dataplane.ValidationResult{
		MessageID:     "msg-1",
		Binding:       "orders",
		Topic:         "mr.orders",
		CorrelationID: "corr-1",
		Status:        dataplane.ResultStatusFailed,
		ProcessedAt:   10,
	}); p != nil {
		t.Fatalf("save result: %v", p)
	}

	srv := NewServer(nil, nil, ":0", false, nil, WithDataPlane(runtime, results, fakeEmitter{
		message: dataplane.Message{
			MessageID:  "msg-2",
			Binding:    "orders",
			Topic:      "mr.orders",
			ProducedAt: 10,
			Payload:    json.RawMessage(`{"status":"OPEN"}`),
		},
	}))

	body := bytes.NewBufferString(`{"name":"orders","kafka_topic":"mr.orders"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/dataplane/bindings", body)
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("upsert binding status=%d want=%d", rec.Code, http.StatusAccepted)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/dataplane/results?binding=orders", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get results status=%d want=%d", rec.Code, http.StatusOK)
	}
	var payload map[string][]dataplane.ValidationResult
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode results body: %v", err)
	}
	if len(payload["results"]) != 1 {
		t.Fatalf("results=%d want=1", len(payload["results"]))
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/dataplane/results?correlation_id=corr-1", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get results by correlation status=%d want=%d", rec.Code, http.StatusOK)
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode correlation results body: %v", err)
	}
	if len(payload["results"]) != 1 || payload["results"][0].CorrelationID != "corr-1" {
		t.Fatalf("correlation results=%+v want corr-1 entry", payload["results"])
	}
}

func TestDataPlaneHandlers_EmitScenario(t *testing.T) {
	store := newDataplaneStore()
	runtime := runtimebootstrap.New(store)
	srv := NewServer(nil, nil, ":0", false, nil, WithDataPlane(runtime, nil, fakeEmitter{
		message: dataplane.Message{
			MessageID:  "msg-2",
			Binding:    "orders",
			Topic:      "mr.orders",
			ProducedAt: 10,
			Payload:    json.RawMessage(`{"status":"OPEN"}`),
		},
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/dataplane/emulator/emit", bytes.NewBufferString(`{"binding":"orders","scenario":"valid"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("emit status=%d want=%d", rec.Code, http.StatusAccepted)
	}
}
