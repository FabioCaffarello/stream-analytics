package httpserver

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/market-raccoon/internal/application/dataplane"
	"github.com/market-raccoon/internal/shared/problem"
)

func (s *Server) handleListDataPlaneBindings(w http.ResponseWriter, r *http.Request) {
	snapshot, p := s.dataPlaneRuntime.Snapshot(r.Context())
	if p != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
		return
	}
	type bindingDTO struct {
		Name                string `json:"name"`
		KafkaTopic          string `json:"kafka_topic"`
		ActiveConfigName    string `json:"active_config_name,omitempty"`
		ActiveConfigVersion string `json:"active_config_version,omitempty"`
	}
	items := make([]bindingDTO, 0, len(snapshot.Bindings))
	for _, binding := range snapshot.Bindings {
		item := bindingDTO{
			Name:       binding.Name,
			KafkaTopic: binding.KafkaTopic,
		}
		if active, ok := snapshot.Active[binding.Name]; ok {
			item.ActiveConfigName = active.Name
			item.ActiveConfigVersion = active.Version
		}
		items = append(items, item)
	}
	writeResponse(w, r, http.StatusOK, "dataplane.bindings", map[string]any{"bindings": items})
}

func (s *Server) handleUpsertDataPlaneBinding(w http.ResponseWriter, r *http.Request) {
	if !supportedRequestContentType(r.Header.Get("Content-Type")) {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}
	var binding dataplane.Binding
	if err := json.NewDecoder(r.Body).Decode(&binding); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if p := s.dataPlaneRuntime.UpsertBinding(r.Context(), binding); p != nil {
		code := http.StatusBadRequest
		if p.Code == problem.Conflict {
			code = http.StatusConflict
		}
		writeJSON(w, code, map[string]string{"error": p.Message, "code": string(p.Code)})
		return
	}
	writeResponse(w, r, http.StatusAccepted, "dataplane.binding.upserted", binding)
}

func (s *Server) handleListDataPlaneConfigs(w http.ResponseWriter, r *http.Request) {
	binding := r.URL.Query().Get("binding")
	activeOnly := r.URL.Query().Get("active") == "true"
	if activeOnly {
		if binding == "" {
			snapshot, p := s.dataPlaneRuntime.Snapshot(r.Context())
			if p != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
				return
			}
			writeResponse(w, r, http.StatusOK, "dataplane.configs.active", snapshot.Active)
			return
		}
		cfg, p := s.dataPlaneRuntime.ActiveConfig(r.Context(), binding)
		if p != nil {
			code := http.StatusInternalServerError
			if p.Code == problem.NotFound {
				code = http.StatusNotFound
			}
			writeJSON(w, code, map[string]string{"error": p.Message, "code": string(p.Code)})
			return
		}
		writeResponse(w, r, http.StatusOK, "dataplane.config.active", cfg)
		return
	}
	configs, p := s.dataPlaneRuntime.Configs(r.Context(), binding)
	if p != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message})
		return
	}
	writeResponse(w, r, http.StatusOK, "dataplane.configs", map[string]any{"configs": configs})
}

func (s *Server) handleUpsertDataPlaneConfig(w http.ResponseWriter, r *http.Request) {
	if !supportedRequestContentType(r.Header.Get("Content-Type")) {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}
	var cfg dataplane.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if p := s.dataPlaneRuntime.UpsertConfig(r.Context(), cfg); p != nil {
		code := http.StatusBadRequest
		if p.Code == problem.NotFound {
			code = http.StatusNotFound
		}
		writeJSON(w, code, map[string]string{"error": p.Message, "code": string(p.Code)})
		return
	}
	writeResponse(w, r, http.StatusAccepted, "dataplane.config.upserted", cfg)
}

func (s *Server) handleActivateDataPlaneConfig(w http.ResponseWriter, r *http.Request) {
	if !supportedRequestContentType(r.Header.Get("Content-Type")) {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}
	var activation dataplane.Activation
	if err := json.NewDecoder(r.Body).Decode(&activation); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if activation.ActivatedAt <= 0 {
		activation.ActivatedAt = time.Now().UnixMilli()
	}
	if p := s.dataPlaneRuntime.ActivateConfig(r.Context(), activation); p != nil {
		code := http.StatusBadRequest
		if p.Code == problem.NotFound {
			code = http.StatusNotFound
		}
		writeJSON(w, code, map[string]string{"error": p.Message, "code": string(p.Code)})
		return
	}
	writeResponse(w, r, http.StatusAccepted, "dataplane.config.activated", activation)
}

func (s *Server) handleGetDataPlaneResults(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	query := dataplane.ResultQuery{
		Binding:       r.URL.Query().Get("binding"),
		MessageID:     r.URL.Query().Get("message_id"),
		CorrelationID: r.URL.Query().Get("correlation_id"),
		Limit:         limit,
	}
	if query.MessageID != "" {
		result, p := s.dataPlaneResults.Get(r.Context(), query.MessageID)
		if p != nil {
			code := http.StatusInternalServerError
			if p.Code == problem.NotFound {
				code = http.StatusNotFound
			}
			writeJSON(w, code, map[string]string{"error": p.Message, "code": string(p.Code)})
			return
		}
		writeResponse(w, r, http.StatusOK, "dataplane.result", result)
		return
	}
	results, p := s.dataPlaneResults.List(r.Context(), query)
	if p != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": p.Message, "code": string(p.Code)})
		return
	}
	writeResponse(w, r, http.StatusOK, "dataplane.results", map[string]any{"results": results})
}

func (s *Server) handleEmitDataPlaneScenario(w http.ResponseWriter, r *http.Request) {
	if !supportedRequestContentType(r.Header.Get("Content-Type")) {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}
	var req struct {
		Binding  string `json:"binding"`
		Scenario string `json:"scenario"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	msg, p := s.dataPlaneEmitter.Emit(r.Context(), req.Binding, req.Scenario)
	if p != nil {
		code := http.StatusBadRequest
		if p.Code == problem.NotFound {
			code = http.StatusNotFound
		}
		writeJSON(w, code, map[string]string{"error": p.Message, "code": string(p.Code)})
		return
	}
	writeResponse(w, r, http.StatusAccepted, "dataplane.emulator.emitted", msg)
}
