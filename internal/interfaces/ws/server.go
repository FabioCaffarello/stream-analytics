package wsserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/anthdm/hollywood/actor"
	"github.com/gorilla/websocket"
	deliveryruntime "github.com/market-raccoon/internal/actors/delivery/runtime"
	"github.com/market-raccoon/internal/core/delivery/ports"
	sharedclock "github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/ids"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/observability"
	"github.com/market-raccoon/internal/shared/problem"
)

// ConnectionLimits controls hard caps for websocket sessions and subscriptions.
type ConnectionLimits struct {
	MaxConnectionsPerIP  int
	MaxConnectionsPerKey int
	MaxSubsPerConnection int
	MaxSymbolsPerConn    int
}

// Server upgrades HTTP connections to WebSocket and delegates lifecycle to SessionActor.
type Server struct {
	engine                  *actor.Engine
	routerPID               *actor.PID
	logger                  *slog.Logger
	upgrader                websocket.Upgrader
	rangeStore              ports.RangeStore
	outboundQueueSize       int
	slowClientDropThreshold int
	auth                    AuthConfig
	rateLimit               deliveryruntime.RateLimitConfig
	ipRateLimit             deliveryruntime.RateLimitConfig
	transcodeCache          *deliveryruntime.TranscodeCache
	spawnSession            func(cfg deliveryruntime.SessionConfig) *actor.PID
	limits                  ConnectionLimits
	serverInstanceID        string
	connRegistry            *connectionRegistry
	ipLimiter               *ipRateLimiter
}

type connectionRegistry struct {
	mu    sync.Mutex
	byIP  map[string]int
	byKey map[string]int
	total int
}

type ipRateLimiter struct {
	mu      sync.Mutex
	clock   sharedclock.Clock
	cfg     deliveryruntime.RateLimitConfig
	buckets map[string]*deliveryruntime.RateLimiter
}

type wsClientMode string

const (
	wsClientModeV1     wsClientMode = "v1"
	wsClientModeLegacy wsClientMode = "legacy"
)

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

func WithIPRateLimit(cfg deliveryruntime.RateLimitConfig) Option {
	return func(s *Server) {
		s.ipRateLimit = cfg
	}
}

func WithConnectionLimits(limits ConnectionLimits) Option {
	return func(s *Server) {
		s.limits = limits
	}
}

func WithServerInstanceID(id string) Option {
	return func(s *Server) {
		id = strings.TrimSpace(id)
		if id != "" {
			s.serverInstanceID = id
		}
	}
}

func WithSlowClientDropThreshold(threshold int) Option {
	return func(s *Server) {
		s.slowClientDropThreshold = threshold
	}
}

func WithTranscodeCache(cache *deliveryruntime.TranscodeCache) Option {
	return func(s *Server) {
		s.transcodeCache = cache
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
		limits: ConnectionLimits{
			MaxConnectionsPerIP:  200,
			MaxConnectionsPerKey: 20,
			MaxSubsPerConnection: 256,
			MaxSymbolsPerConn:    128,
		},
		serverInstanceID: ids.NewSessionID().String(),
		connRegistry: &connectionRegistry{
			byIP:  map[string]int{},
			byKey: map[string]int{},
		},
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
	if srv.ipRateLimit.Enabled {
		srv.ipLimiter = &ipRateLimiter{
			clock:   sharedclock.NewSystemClock(),
			cfg:     srv.ipRateLimit,
			buckets: map[string]*deliveryruntime.RateLimiter{},
		}
	}
	return srv
}

func (s *Server) HandleUpgrade(w http.ResponseWriter, r *http.Request) {
	s.handleUpgradeWithMode(w, r, wsClientModeFromRequestPath(r))
}

func (s *Server) handleUpgradeWithMode(w http.ResponseWriter, r *http.Request, mode wsClientMode) {
	if s.routerPID == nil {
		http.Error(w, "delivery router unavailable", http.StatusServiceUnavailable)
		return
	}
	clientIP := requestClientIP(r)
	if s.ipLimiter != nil && !s.ipLimiter.Allow(clientIP) {
		metrics.IncWSAuthFail()
		observability.IncTerminalWSAuthFail()
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	principal, p := s.auth.Authenticate(r)
	if p != nil {
		metrics.IncWSQueryRejected("unauthorized")
		metrics.IncWSAuthFail()
		observability.IncTerminalWSAuthFail()
		http.Error(w, p.Message, http.StatusUnauthorized)
		return
	}
	if !principal.HasScope(wsScopeRead) {
		metrics.IncWSQueryRejected("forbidden")
		metrics.IncWSAuthFail()
		observability.IncTerminalWSAuthFail()
		http.Error(w, "missing read scope", http.StatusForbidden)
		return
	}

	keyID := principalKey(principal)
	if denial := s.connRegistry.Acquire(clientIP, keyID, s.limits); denial != nil {
		metrics.IncWSQueryRejected("connection_limit")
		http.Error(w, denial.Message, http.StatusTooManyRequests)
		return
	}
	syncConnectionIntrospection(s.connRegistry)

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.connRegistry.Release(clientIP, keyID)
		syncConnectionIntrospection(s.connRegistry)
		s.logger.Warn("ws upgrade failed", "err", err)
		return
	}

	metrics.IncWSClientsConnectedByMode(string(mode))
	onClosed := func() {
		metrics.DecWSClientsConnectedByMode(string(mode))
		s.connRegistry.Release(clientIP, keyID)
		syncConnectionIntrospection(s.connRegistry)
	}

	pid := s.spawnSession(deliveryruntime.SessionConfig{
		Logger:                  s.logger,
		RouterPID:               s.routerPID,
		Conn:                    conn,
		ClientID:                principal.ClientID,
		TenantID:                principal.TenantID,
		RangeStore:              s.rangeStore,
		OutboundQueueSize:       s.outboundQueueSize,
		SlowClientDropThreshold: s.slowClientDropThreshold,
		PreferProto:             sessionWantsProto(r),
		RateLimit:               s.rateLimit,
		TranscodeCache:          s.transcodeCache,
		ServerInstanceID:        s.serverInstanceID,
		MaxSubscriptions:        s.limits.MaxSubsPerConnection,
		MaxSymbolsPerConnection: s.limits.MaxSymbolsPerConn,
		OnClosed:                onClosed,
	})
	if pid == nil {
		onClosed()
		_ = conn.Close()
		http.Error(w, "session spawn failed", http.StatusServiceUnavailable)
		return
	}
}

func (s *Server) HandleWS(w http.ResponseWriter, r *http.Request) {
	s.handleUpgradeWithMode(w, r, wsClientModeV1)
}

// HandleLegacyWS keeps backward-compatible route handling isolated from
// Terminal V1 route handling and instrumentation.
func (s *Server) HandleLegacyWS(w http.ResponseWriter, r *http.Request) {
	s.handleUpgradeWithMode(w, r, wsClientModeLegacy)
}

func (s *Server) HandleIntrospection(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"server_instance_id": s.serverInstanceID,
		"ws":                 observability.SnapshotTerminalWSState(256),
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "failed to encode introspection", http.StatusInternalServerError)
	}
}

func requestClientIP(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			if ip := strings.TrimSpace(parts[0]); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		if v := strings.TrimSpace(r.RemoteAddr); v != "" {
			return v
		}
		return "unknown"
	}
	if host == "" {
		return "unknown"
	}
	return host
}

func principalKey(pr Principal) string {
	if pr.APIKey != "" {
		hash := sha256Hex(pr.APIKey)
		return "key:" + hash
	}
	if pr.ClientID != "" {
		return "client:" + strings.ToLower(strings.TrimSpace(pr.ClientID))
	}
	return "anonymous"
}

func sha256Hex(value string) string {
	// Keep key identity opaque in logs/introspection while deterministic.
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:8])
}

func syncConnectionIntrospection(reg *connectionRegistry) {
	if reg == nil {
		return
	}
	observability.SetTerminalWSConnectionsActive(int64(reg.Total()))
}

func (r *connectionRegistry) Acquire(ip, key string, limits ConnectionLimits) *problem.Problem {
	if r == nil {
		return nil
	}
	if ip == "" {
		ip = "unknown"
	}
	if key == "" {
		key = "anonymous"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if limits.MaxConnectionsPerIP > 0 && r.byIP[ip] >= limits.MaxConnectionsPerIP {
		return problem.Newf(problem.Unavailable, "max connections per IP exceeded (%d)", limits.MaxConnectionsPerIP)
	}
	if limits.MaxConnectionsPerKey > 0 && r.byKey[key] >= limits.MaxConnectionsPerKey {
		return problem.Newf(problem.Unavailable, "max connections per key exceeded (%d)", limits.MaxConnectionsPerKey)
	}
	r.byIP[ip]++
	r.byKey[key]++
	r.total++
	return nil
}

func (r *connectionRegistry) Release(ip, key string) {
	if r == nil {
		return
	}
	if ip == "" {
		ip = "unknown"
	}
	if key == "" {
		key = "anonymous"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if count := r.byIP[ip]; count > 1 {
		r.byIP[ip] = count - 1
	} else {
		delete(r.byIP, ip)
	}
	if count := r.byKey[key]; count > 1 {
		r.byKey[key] = count - 1
	} else {
		delete(r.byKey, key)
	}
	if r.total > 0 {
		r.total--
	}
}

func (r *connectionRegistry) Total() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.total
}

func (l *ipRateLimiter) Allow(ip string) bool {
	if l == nil || !l.cfg.Enabled {
		return true
	}
	if ip == "" {
		ip = "unknown"
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	limiter, ok := l.buckets[ip]
	if !ok || limiter == nil {
		limiter = deliveryruntime.NewRateLimiter(l.cfg.BurstCapacity, l.cfg.MaxPerSecond, l.clock)
		l.buckets[ip] = limiter
	}
	return limiter.Allow()
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

func wsClientModeFromRequestPath(r *http.Request) wsClientMode {
	if r == nil {
		return wsClientModeV1
	}
	switch strings.ToLower(strings.TrimSpace(r.URL.Path)) {
	case "/ws/marketdata":
		return wsClientModeLegacy
	default:
		return wsClientModeV1
	}
}
