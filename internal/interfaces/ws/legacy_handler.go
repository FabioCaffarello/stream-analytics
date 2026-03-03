package wsserver

import (
	"net/http"

	"github.com/market-raccoon/internal/shared/metrics"
)

// HandleLegacyWS is the only backend compatibility choke point for legacy WS.
// Runtime V1 (router/session) remains shared and unaware of the legacy route.
func (s *Server) HandleLegacyWS(w http.ResponseWriter, r *http.Request) {
	if !s.allowLegacy {
		metrics.IncWSLegacyRequest("rejected")
		http.Error(w, "legacy route deprecated; use /ws", http.StatusGone)
		return
	}
	metrics.IncWSLegacyRequest("accepted")
	if s.logger != nil {
		s.logger.Warn(
			"ws legacy route used",
			"path", "/ws/marketdata",
			"remote_addr", requestClientIP(r),
		)
	}
	s.handleUpgradeWithMode(w, r, wsClientModeLegacy)
}
