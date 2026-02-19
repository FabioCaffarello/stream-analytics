package wsserver

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/anthdm/hollywood/actor"
	"github.com/gorilla/websocket"
	deliveryruntime "github.com/market-raccoon/internal/actors/delivery/runtime"
	"github.com/market-raccoon/internal/core/delivery/ports"
	"github.com/market-raccoon/internal/shared/metrics"
)

// Server upgrades HTTP connections to WebSocket and delegates lifecycle to SessionActor.
type Server struct {
	engine            *actor.Engine
	routerPID         *actor.PID
	logger            *slog.Logger
	upgrader          websocket.Upgrader
	rangeStore        ports.RangeStore
	outboundQueueSize int
	auth              AuthConfig
	rateLimit         deliveryruntime.RateLimitConfig
	spawnSession      func(cfg deliveryruntime.SessionConfig) *actor.PID
}

type Option func(*Server)

func WithAuthConfig(cfg AuthConfig) Option {
	return func(s *Server) {
		s.auth = cfg
	}
}

func WithRateLimit(cfg deliveryruntime.RateLimitConfig) Option {
	return func(s *Server) {
		s.rateLimit = cfg
	}
}

func WithSessionSpawner(spawn func(cfg deliveryruntime.SessionConfig) *actor.PID) Option {
	return func(s *Server) {
		s.spawnSession = spawn
	}
}

func NewServer(engine *actor.Engine, routerPID *actor.PID, logger *slog.Logger, rangeStore ports.RangeStore, outboundQueueSize int, opts ...Option) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	srv := &Server{
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
	for _, opt := range opts {
		if opt != nil {
			opt(srv)
		}
	}
	if srv.spawnSession == nil {
		srv.spawnSession = func(cfg deliveryruntime.SessionConfig) *actor.PID {
			if srv.engine == nil {
				return nil
			}
			return srv.engine.Spawn(
				deliveryruntime.NewSessionActor(cfg),
				"delivery-session",
			)
		}
	}
	return srv
}

func (s *Server) HandleUpgrade(w http.ResponseWriter, r *http.Request) {
	if s.routerPID == nil {
		http.Error(w, "delivery router unavailable", http.StatusServiceUnavailable)
		return
	}
	clientID, p := s.auth.Authenticate(r)
	if p != nil {
		metrics.IncWSQueryRejected("unauthorized")
		http.Error(w, p.Message, http.StatusUnauthorized)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Warn("ws upgrade failed", "err", err)
		return
	}

	s.spawnSession(deliveryruntime.SessionConfig{
		Logger:            s.logger,
		RouterPID:         s.routerPID,
		Conn:              conn,
		ClientID:          clientID,
		RangeStore:        s.rangeStore,
		OutboundQueueSize: s.outboundQueueSize,
		PreferProto:       sessionWantsProto(r),
		RateLimit:         s.rateLimit,
	})
}

func (s *Server) HandleWS(w http.ResponseWriter, r *http.Request) {
	s.HandleUpgrade(w, r)
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
