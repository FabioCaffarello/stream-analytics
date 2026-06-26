package wsserver

import (
	"net/http"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/metrics"
)

// HandleLegacyWS is the legacy compatibility choke point.
// Hard cutover: legacy route is permanently disabled and returns HTTP 410.
func (s *Server) HandleLegacyWS(w http.ResponseWriter, r *http.Request) {
	metrics.IncWSLegacyRequest("rejected")
	if s.logger != nil {
		s.logger.Warn(
			"ws legacy route rejected",
			"path", "/ws/marketdata",
			"remote_addr", requestClientIP(r),
		)
	}
	http.Error(w, "legacy route removed; use /ws", http.StatusGone)
}
