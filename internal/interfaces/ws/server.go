package wsserver

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/anthdm/hollywood/actor"
	"github.com/gorilla/websocket"
	deliveryruntime "github.com/market-raccoon/internal/actors/delivery/runtime"
	"github.com/market-raccoon/internal/core/delivery/ports"
)

// Server upgrades HTTP connections to WebSocket and delegates lifecycle to SessionActor.
type Server struct {
	engine            *actor.Engine
	routerPID         *actor.PID
	logger            *slog.Logger
	upgrader          websocket.Upgrader
	rangeStore        ports.RangeStore
	outboundQueueSize int
}

func NewServer(engine *actor.Engine, routerPID *actor.PID, logger *slog.Logger, rangeStore ports.RangeStore, outboundQueueSize int) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		engine:            engine,
		routerPID:         routerPID,
		logger:            logger,
		rangeStore:        rangeStore,
		outboundQueueSize: outboundQueueSize,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
}

func (s *Server) HandleWS(w http.ResponseWriter, r *http.Request) {
	if s.routerPID == nil {
		http.Error(w, "delivery router unavailable", http.StatusServiceUnavailable)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Warn("ws upgrade failed", "err", err)
		return
	}

	s.engine.Spawn(
		deliveryruntime.NewSessionActor(deliveryruntime.SessionConfig{
			Logger:            s.logger,
			RouterPID:         s.routerPID,
			Conn:              conn,
			RangeStore:        s.rangeStore,
			OutboundQueueSize: s.outboundQueueSize,
			PreferProto:       sessionWantsProto(r),
		}),
		"delivery-session",
	)
}

func sessionWantsProto(r *http.Request) bool {
	if r == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("format")), "proto") {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Delivery-Format")), "proto") {
		return true
	}
	// Optional handshake hint for clients that use ws subprotocol negotiation.
	for _, token := range strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
		if strings.EqualFold(strings.TrimSpace(token), "proto") {
			return true
		}
	}
	return false
}
