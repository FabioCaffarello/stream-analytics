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
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/gorilla/websocket"
	deliveryruntime "github.com/market-raccoon/internal/actors/delivery/runtime"
	"github.com/market-raccoon/internal/core/delivery/ports"
	sharedclock "github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/ids"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/observability"
	"github.com/market-raccoon/internal/shared/problem"
)

// ServerConnectionLimits controls hard caps for websocket sessions and subscriptions.
type ServerConnectionLimits struct {
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
	snapshotWireCache       *deliveryruntime.SnapshotWireCache
	spawnSession            func(cfg deliveryruntime.SessionConfig) *actor.PID
	limits                  ServerConnectionLimits
	serverInstanceID        string
	connRegistry            *connectionRegistry
	ipLimiter               *ipRateLimiter
	tenantLimits            map[string]config.WSTenantLimitConfig
	maxFrameBytes           int
	allowLegacy             bool
	enableCompression       bool
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
	buckets map[string]ipRateLimiterBucket

	maxEntries int
	idleTTL    time.Duration
	sweepEvery int
	calls      int
}

type ipRateLimiterBucket struct {
	limiter  *deliveryruntime.RateLimiter
	lastSeen time.Time
}

type wsClientMode string

const (
	wsClientModeV1     wsClientMode = "v1"
	wsClientModeLegacy wsClientMode = "legacy"

	defaultIPRateLimiterMaxEntries = 8192
	defaultIPRateLimiterIdleTTL    = 15 * time.Minute
	defaultIPRateLimiterSweepEvery = 256
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

func WithConnectionLimits(limits ServerConnectionLimits) Option {
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

func WithTenantLimits(limits map[string]config.WSTenantLimitConfig) Option {
	return func(s *Server) {
		s.tenantLimits = limits
	}
}

func WithMaxFrameBytes(maxFrameBytes int) Option {
	return func(s *Server) {
		s.maxFrameBytes = maxFrameBytes
	}
}

func WithAllowLegacy(allow bool) Option {
	return func(s *Server) {
		s.allowLegacy = allow
	}
}

func WithCompressionEnabled(enabled bool) Option {
	return func(s *Server) {
		s.enableCompression = enabled
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
		allowLegacy:       true,
		enableCompression: true,
		limits: ServerConnectionLimits{
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
			ReadBufferSize:    4096,
			WriteBufferSize:   4096,
			EnableCompression: true,
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
	srv.upgrader.EnableCompression = srv.enableCompression
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
			clock:      sharedclock.NewSystemClock(),
			cfg:        srv.ipRateLimit,
			buckets:    map[string]ipRateLimiterBucket{},
			maxEntries: defaultIPRateLimiterMaxEntries,
			idleTTL:    defaultIPRateLimiterIdleTTL,
			sweepEvery: defaultIPRateLimiterSweepEvery,
		}
	}
	if srv.snapshotWireCache == nil {
		srv.snapshotWireCache = deliveryruntime.NewSnapshotWireCache(0, 0)
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

	sessionRateLimit := s.rateLimit
	maxSubs := s.limits.MaxSubsPerConnection
	maxSymbols := s.limits.MaxSymbolsPerConn
	maxFrameBytes := s.maxFrameBytes
	outboundQueueSize := s.outboundQueueSize
	if tl, ok := s.tenantLimits[principal.TenantID]; ok {
		if tl.MaxSubsPerConnection > 0 {
			maxSubs = tl.MaxSubsPerConnection
		}
		if tl.MaxSymbolsPerConn > 0 {
			maxSymbols = tl.MaxSymbolsPerConn
		}
		if tl.MaxFrameBytes > 0 {
			maxFrameBytes = tl.MaxFrameBytes
		}
		if tl.OutboundQueueSize > 0 {
			outboundQueueSize = tl.OutboundQueueSize
		}
		if hasRateLimitOverride(tl.RateLimit) {
			sessionRateLimit = deliveryruntime.RateLimitConfig{
				Enabled:       tl.RateLimit.Enabled,
				MaxPerSecond:  tl.RateLimit.MaxPerSecond,
				BurstCapacity: tl.RateLimit.BurstCapacity,
			}
		}
	}
	sessionRateLimit = normalizeRateLimit(sessionRateLimit, s.rateLimit)
	pid := s.spawnSession(deliveryruntime.SessionConfig{
		Logger:                  s.logger,
		RouterPID:               s.routerPID,
		Conn:                    conn,
		ClientID:                principal.ClientID,
		TenantID:                principal.TenantID,
		RangeStore:              s.rangeStore,
		OutboundQueueSize:       outboundQueueSize,
		SlowClientDropThreshold: s.slowClientDropThreshold,
		PreferProto:             sessionWantsProto(r),
		RateLimit:               sessionRateLimit,
		TranscodeCache:          s.transcodeCache,
		SnapshotWireCache:       s.snapshotWireCache,
		CompressionEnabled:      s.enableCompression,
		ServerInstanceID:        s.serverInstanceID,
		MaxSubscriptions:        maxSubs,
		MaxSymbolsPerConnection: maxSymbols,
		MaxFrameBytes:           maxFrameBytes,
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

func (s *Server) HandleIntrospection(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	snap := observability.SnapshotTerminalWSState(256)
	resp := map[string]any{
		"server_instance_id":   s.serverInstanceID,
		"sessions_active":      snap.ConnectionsActive,
		"subscriptions_active": snap.SubscriptionsActive,
		"drops_total":          snap.DropsTotal,
		"serialize_errors":     snap.SerializeErrorsTotal,
		"resync_total":         snap.ResyncTotal,
		"auth_fail_total":      snap.AuthFailTotal,
		"streams":              snap.Streams,
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

func hasRateLimitOverride(cfg config.WSRateLimitConfig) bool {
	return cfg.Enabled || cfg.MaxPerSecond > 0 || cfg.BurstCapacity > 0
}

func normalizeRateLimit(override, fallback deliveryruntime.RateLimitConfig) deliveryruntime.RateLimitConfig {
	out := override
	if !out.Enabled {
		return out
	}
	if out.MaxPerSecond <= 0 {
		if fallback.MaxPerSecond > 0 {
			out.MaxPerSecond = fallback.MaxPerSecond
		} else {
			out.MaxPerSecond = 100
		}
	}
	if out.BurstCapacity <= 0 {
		if fallback.BurstCapacity > 0 {
			out.BurstCapacity = fallback.BurstCapacity
		} else {
			out.BurstCapacity = 200
		}
	}
	return out
}

func (r *connectionRegistry) Acquire(ip, key string, limits ServerConnectionLimits) *problem.Problem {
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
	now := l.clock.Now()
	l.calls++
	if l.sweepEvery > 0 && l.calls%l.sweepEvery == 0 {
		l.evictExpiredLocked(now)
	}

	bucket, ok := l.buckets[ip]
	if !ok || bucket.limiter == nil {
		if l.maxEntries > 0 && len(l.buckets) >= l.maxEntries {
			l.evictExpiredLocked(now)
			for len(l.buckets) >= l.maxEntries {
				if !l.evictOldestLocked() {
					break
				}
			}
		}
		bucket = ipRateLimiterBucket{
			limiter:  deliveryruntime.NewRateLimiter(l.cfg.BurstCapacity, l.cfg.MaxPerSecond, l.clock),
			lastSeen: now,
		}
	} else {
		bucket.lastSeen = now
	}
	allow := bucket.limiter.Allow()
	bucket.lastSeen = now
	l.buckets[ip] = bucket
	return allow
}

func (l *ipRateLimiter) evictExpiredLocked(now time.Time) {
	if l == nil || l.idleTTL <= 0 {
		return
	}
	for ip, bucket := range l.buckets {
		if bucket.lastSeen.IsZero() {
			delete(l.buckets, ip)
			continue
		}
		if now.Sub(bucket.lastSeen) > l.idleTTL {
			delete(l.buckets, ip)
		}
	}
}

func (l *ipRateLimiter) evictOldestLocked() bool {
	if l == nil || len(l.buckets) == 0 {
		return false
	}
	var (
		oldestIP string
		oldestTS time.Time
		found    bool
	)
	for ip, bucket := range l.buckets {
		if !found || bucket.lastSeen.Before(oldestTS) {
			oldestIP = ip
			oldestTS = bucket.lastSeen
			found = true
		}
	}
	if !found {
		return false
	}
	delete(l.buckets, oldestIP)
	return true
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
