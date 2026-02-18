package wsserver

import (
	"net/http"
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
)

// AuthConfig controls websocket API-key authentication.
type AuthConfig struct {
	Enabled bool
	APIKeys map[string]string // api_key -> client_id
}

func (cfg AuthConfig) Authenticate(r *http.Request) (string, *problem.Problem) {
	if !cfg.Enabled {
		return "anonymous", nil
	}
	key := ""
	if r != nil && r.URL != nil {
		key = strings.TrimSpace(r.URL.Query().Get("api_key"))
	}
	if key == "" && r != nil {
		key = strings.TrimSpace(r.Header.Get("X-API-Key"))
	}
	if key == "" && r != nil {
		key = bearerToken(strings.TrimSpace(r.Header.Get("Authorization")))
	}
	if key == "" {
		return "", problem.New(problem.ValidationFailed, "missing API key")
	}
	clientID, ok := cfg.APIKeys[key]
	if !ok {
		return "", problem.New(problem.ValidationFailed, "invalid API key")
	}
	return clientID, nil
}

func bearerToken(raw string) string {
	if raw == "" {
		return ""
	}
	parts := strings.SplitN(raw, " ", 2)
	if len(parts) != 2 {
		return ""
	}
	if !strings.EqualFold(strings.TrimSpace(parts[0]), "bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
