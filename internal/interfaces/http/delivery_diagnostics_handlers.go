package httpserver

import (
	"net/http"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/observability"
)

// DeliveryDiagnosticsResponse provides per-subject delivery sequence state
// for operational debugging (gap detection, drop analysis, resync tracking).
type DeliveryDiagnosticsResponse struct {
	ConnectionsActive int64                            `json:"connections_active"`
	ResyncTotal       uint64                           `json:"resync_total"`
	DropsTotal        uint64                           `json:"drops_total"`
	StreamCount       int                              `json:"stream_count"`
	Streams           []DeliveryDiagnosticsStreamState `json:"streams"`
}

// DeliveryDiagnosticsStreamState is a per-subject diagnostic entry.
type DeliveryDiagnosticsStreamState struct {
	StreamID       string `json:"stream_id"`
	Venue          string `json:"venue"`
	Symbol         string `json:"symbol"`
	Channel        string `json:"channel"`
	LastSeq        int64  `json:"last_seq"`
	LastTsIngest   int64  `json:"last_ts_ingest"`
	LastTsServer   int64  `json:"last_ts_server"`
	LagMs          int64  `json:"lag_ms"`
	DeliveredTotal uint64 `json:"delivered_total"`
	DroppedTotal   uint64 `json:"dropped_total"`
	ResyncTotal    uint64 `json:"resync_total"`
}

const deliveryDiagnosticsMaxStreams = 2048

// handleDeliveryDiagnostics serves GET /api/v1/delivery/diagnostics.
//
// Returns per-subject delivery sequence state derived from the terminal WS
// observability store. Useful for ops to see gap stats, drop counts, and
// per-stream seq position without connecting via WS.
func (s *Server) handleDeliveryDiagnostics(w http.ResponseWriter, _ *http.Request) {
	snapshot := observability.SnapshotTerminalWSState(deliveryDiagnosticsMaxStreams)

	streams := make([]DeliveryDiagnosticsStreamState, 0, len(snapshot.Streams))
	for _, st := range snapshot.Streams {
		streams = append(streams, DeliveryDiagnosticsStreamState{
			StreamID:       st.StreamID,
			Venue:          st.Venue,
			Symbol:         st.Symbol,
			Channel:        st.Channel,
			LastSeq:        st.LastSeq,
			LastTsIngest:   st.LastTsIngest,
			LastTsServer:   st.LastTsServer,
			LagMs:          st.LastLagMs,
			DeliveredTotal: st.DeliveredTotal,
			DroppedTotal:   st.DroppedTotal,
			ResyncTotal:    st.ResyncTotal,
		})
	}

	writeJSON(w, http.StatusOK, DeliveryDiagnosticsResponse{
		ConnectionsActive: snapshot.ConnectionsActive,
		ResyncTotal:       snapshot.ResyncTotal,
		DropsTotal:        snapshot.DropsTotal,
		StreamCount:       len(streams),
		Streams:           streams,
	})
}
