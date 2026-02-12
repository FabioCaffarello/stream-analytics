package wsserver

import (
	"log/slog"
	"net/http"

	"github.com/anthdm/hollywood/actor"
	"github.com/gorilla/websocket"
	deliveryruntime "github.com/market-raccoon/internal/actors/delivery/runtime"
)

// Server upgrades HTTP connections to WebSocket and delegates lifecycle to SessionActor.
type Server struct {
	engine    *actor.Engine
	routerPID *actor.PID
	logger    *slog.Logger
	upgrader  websocket.Upgrader
}

func NewServer(engine *actor.Engine, routerPID *actor.PID, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		engine:    engine,
		routerPID: routerPID,
		logger:    logger,
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
			Logger:    s.logger,
			RouterPID: s.routerPID,
			Conn:      conn,
		}),
		"delivery-session",
	)
}
