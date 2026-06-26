package httpserver

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// maxWorkspaceBodyBytes limits the PUT request body to 1 MiB.
const maxWorkspaceBodyBytes = 1 << 20

// workspacePutRequest is the JSON payload for PUT /api/v1/workspace.
type workspacePutRequest struct {
	SchemaVersion int               `json:"schema_version"`
	LayoutV6      string            `json:"layout_v6"`
	Settings      map[string]string `json:"settings"`
	SavedAtMs     int64             `json:"saved_at_ms"`
}

// workspaceGetResponse is the JSON response for GET /api/v1/workspace.
type workspaceGetResponse struct {
	SchemaVersion int               `json:"schema_version"`
	LayoutV6      string            `json:"layout_v6"`
	Fingerprint   string            `json:"fingerprint"`
	Settings      map[string]string `json:"settings"`
	SavedAtMs     int64             `json:"saved_at_ms"`
}

// handleGetWorkspace serves GET /api/v1/workspace.
//
// Returns 200 with the saved workspace state, or 204 if no state exists yet
// (first run).  Returns 501 if no workspace service is configured.
func (s *Server) handleGetWorkspace(w http.ResponseWriter, r *http.Request) {
	if s.workspaceSvc == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{
			"error": "workspace persistence not configured",
		})
		return
	}

	res := s.workspaceSvc.LoadWorkspace()
	if !res.IsOk() {
		writeWorkspaceProblem(w, res.Problem())
		return
	}

	ws := res.Value()
	if ws == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	writeJSON(w, http.StatusOK, workspaceGetResponse{
		SchemaVersion: ws.SchemaVersion(),
		LayoutV6:      ws.LayoutV6(),
		Fingerprint:   ws.Fingerprint(),
		Settings:      ws.Settings(),
		SavedAtMs:     ws.SavedAtMs(),
	})
}

// handlePutWorkspace serves PUT /api/v1/workspace.
//
// Validates and persists the client workspace state.  Returns 200 on success,
// 400 on invalid payload, 409 if the schema version is from the future,
// 413 if the body exceeds the size limit,
// 501 if no workspace service is configured.
func (s *Server) handlePutWorkspace(w http.ResponseWriter, r *http.Request) {
	if s.workspaceSvc == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{
			"error": "workspace persistence not configured",
		})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxWorkspaceBodyBytes)

	var payload workspacePutRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		status := http.StatusBadRequest
		msg := "invalid JSON body"
		// MaxBytesReader returns a specific error when the limit is exceeded.
		if err.Error() == "http: request body too large" {
			status = http.StatusRequestEntityTooLarge
			msg = "request body too large"
		}
		writeJSON(w, status, map[string]string{
			"error": msg,
		})
		return
	}
	// Drain any remaining bytes so MaxBytesReader can detect oversized bodies
	// that were partially decoded.
	_, _ = io.Copy(io.Discard, r.Body)

	res := s.workspaceSvc.SaveWorkspace(
		payload.SchemaVersion,
		payload.LayoutV6,
		payload.Settings,
		payload.SavedAtMs,
	)
	if !res.IsOk() {
		writeWorkspaceProblem(w, res.Problem())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"saved":       res.Value().Saved,
		"fingerprint": res.Value().Fingerprint,
		"saved_at_ms": res.Value().SavedAtMs,
	})
}

// handleDeleteWorkspace serves DELETE /api/v1/workspace.
//
// Resets workspace to first-run state.  Returns 204 on success,
// 501 if no workspace service is configured.
func (s *Server) handleDeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	if s.workspaceSvc == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{
			"error": "workspace persistence not configured",
		})
		return
	}

	res := s.workspaceSvc.ResetWorkspace()
	if !res.IsOk() {
		writeWorkspaceProblem(w, res.Problem())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// writeWorkspaceProblem maps domain problem kinds to HTTP status codes.
func writeWorkspaceProblem(w http.ResponseWriter, p *problem.Problem) {
	status := http.StatusInternalServerError
	switch p.Code {
	case problem.ValidationFailed:
		status = http.StatusBadRequest
	case problem.Conflict:
		status = http.StatusConflict
	}
	writeJSON(w, status, map[string]string{
		"error": p.Message,
	})
}
