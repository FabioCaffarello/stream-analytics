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
	if r != nil {
		key = strings.TrimSpace(r.Header.Get("X-API-Key"))
		// Fallback: browser WebSocket API cannot set custom headers,
		// so accept api_key as a query parameter.
		if key == "" {
			key = strings.TrimSpace(r.URL.Query().Get("api_key"))
		}
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
