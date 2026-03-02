package config

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

const (
	codeNotFound   problem.ProblemCode = "CFG_NOT_FOUND"
	codeParseError problem.ProblemCode = "CFG_PARSE_ERROR"
	codeInvalid    problem.ProblemCode = "CFG_INVALID"
)

var metricsExchangeNamePattern = regexp.MustCompile(`^[a-z0-9_-]{1,24}$`)

// Load reads a JSONC config file and returns an AppConfig with defaults applied.
// If path is empty, Load returns a fully-defaulted AppConfig without reading any file.
// If the file exists but cannot be parsed, Load returns a non-nil *problem.Problem.
func Load(path string) (AppConfig, *problem.Problem) {
	var cfg AppConfig
	if path == "" {
		applyDefaults(&cfg)
		return cfg, nil
	}

	// #nosec G304 -- configuration path is intentionally runtime-configurable.
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return AppConfig{}, problem.Wrap(err, codeNotFound,
				fmt.Sprintf("config file not found: %s", path))
		}
		return AppConfig{}, problem.Wrap(err, codeParseError,
			fmt.Sprintf("could not read config file: %s", path))
	}

	stripped := stripComments(raw)
	if err := json.Unmarshal(stripped, &cfg); err != nil {
		return AppConfig{}, problem.Wrap(err, codeParseError,
			fmt.Sprintf("config JSON parse error in %s", path))
	}

	// Re-apply defaults to fill any fields left at zero by the JSON.
	applyDefaults(&cfg)
	return cfg, nil
}

// Validate checks that all config fields are semantically valid.
// It returns nil if the config is valid.
func (a AppConfig) Validate() *problem.Problem {
	if prob := validateLog(a.Log); prob != nil {
		return prob
	}
	if prob := validateHTTP(a.HTTP); prob != nil {
		return prob
	}
	if prob := validateWS(a.WS); prob != nil {
		return prob
	}
	if prob := validateDelivery(a.Delivery); prob != nil {
		return prob
	}
	if prob := validateShard(a.Shard); prob != nil {
		return prob
	}
	if prob := validateBus(a.Bus); prob != nil {
		return prob
	}
	if prob := validateJetStream(a.Bus, a.JetStream); prob != nil {
		return prob
	}
	if prob := validateConsumer(a.Consumer); prob != nil {
		return prob
	}
	if prob := validateMarketData(a.MarketData); prob != nil {
		return prob
	}
	if prob := validateReplay(a.Bus, a.MarketData, a.Replay); prob != nil {
		return prob
	}
	if prob := validateProcessor(a.Processor); prob != nil {
		return prob
	}
	if prob := validateStore(a.Store); prob != nil {
		return prob
	}
	if prob := validateStorage(a.Storage); prob != nil {
		return prob
	}
	if prob := ValidateFeatureSubjects(a); prob != nil {
		return prob
	}
	if prob := validateCrossField(a); prob != nil {
		return prob
	}
	return nil
}

// ValidateFeatureSubjects enforces fail-fast subject coverage for optional
// feature paths that depend on JetStream subject filters.
func ValidateFeatureSubjects(cfg AppConfig) *problem.Problem {
	joinEnabled := cfg.Processor.Insights.EnableCrossVenueJoin
	replayJetStream := strings.EqualFold(strings.TrimSpace(cfg.Replay.Mode), "jetstream")

	if joinEnabled {
		joinSubject := strings.TrimSpace(cfg.Processor.Insights.JoinTradesSubject)
		if joinSubject == "" {
			return problem.New(codeInvalid, `processor.insights.join_trades_subject must not be empty; e.g. "marketdata.trade.v1.>"`)
		}
		if !isValidNATSSubjectPattern(joinSubject) {
			return problem.Newf(codeInvalid, `processor.insights.join_trades_subject is invalid; e.g. "marketdata.trade.v1.>"`)
		}
		if !matchesAnySubject(joinSubject, expectedTradeSubjects(cfg.Consumer)) {
			return problem.New(codeInvalid, `processor.insights.join_trades_subject must match marketdata.trade subjects; e.g. "marketdata.trade.v1.>"`)
		}
		if p := validateInsightsPublishSubjectPrefix(cfg.Processor.Insights.SnapshotSubjectPrefix); p != nil {
			return p
		}
	}

	if replayJetStream {
		filter := strings.TrimSpace(cfg.Replay.JetStream.SubjectFilter)
		if filter == "" {
			return problem.New(codeInvalid, `replay.jetstream.subject_filter must not be empty; e.g. "marketdata.>"`)
		}
		if !isValidNATSSubjectPattern(filter) {
			return problem.Newf(codeInvalid, `replay.jetstream.subject_filter is invalid; e.g. "marketdata.>"`)
		}
		if !matchesAnySubject(filter, expectedMarketDataSubjects(cfg.Consumer)) {
			return problem.New(codeInvalid, `replay.jetstream.subject_filter must include marketdata subjects; e.g. "marketdata.>"`)
		}
	}

	if !joinEnabled {
		return nil
	}

	patterns, key := runtimeInputSubjectPatterns(cfg)
	if len(patterns) == 0 {
		return nil
	}
	for _, required := range expectedTradeSubjects(cfg.Consumer) {
		if !anyPatternMatchesSubject(patterns, required) {
			return problem.Newf(
				codeInvalid,
				`%s must include trade subjects for all configured exchanges; e.g. "marketdata.trade.v1.>" (missing %q)`,
				key,
				required,
			)
		}
	}
	return nil
}

func validateShard(s ShardConfig) *problem.Problem {
	if s.Count < 1 {
		return problem.Newf(codeInvalid, "shard.count must be >= 1, got %d", s.Count)
	}
	if s.Index < 0 || s.Index >= s.Count {
		return problem.Newf(codeInvalid, "shard.index must be in [0, %d), got %d", s.Count, s.Index)
	}
	if s.MaxLag < 0 {
		return problem.Newf(codeInvalid, "shard.max_lag must be >= 0, got %d", s.MaxLag)
	}
	if s.Registry.Enabled {
		d, err := time.ParseDuration(s.Registry.TopologyGrace)
		if err != nil || d <= 0 {
			return problem.Newf(codeInvalid, "shard.registry.topology_grace must be > 0 duration, got %q", s.Registry.TopologyGrace)
		}
	}
	return nil
}

func validateBus(b BusConfig) *problem.Problem {
	switch strings.ToLower(strings.TrimSpace(b.Type)) {
	case "inmemory", "jetstream":
	default:
		return problem.Newf(codeInvalid, "bus.type must be inmemory|jetstream, got %q", b.Type)
	}
	switch strings.ToLower(strings.TrimSpace(b.WireFormat)) {
	case "", "json", "proto":
		return nil
	default:
		return problem.Newf(codeInvalid, "bus.wire_format must be json|proto, got %q", b.WireFormat)
	}
}

func validateJetStream(bus BusConfig, j JetStreamConfig) *problem.Problem {
	if !strings.EqualFold(strings.TrimSpace(bus.Type), "jetstream") {
		return nil
	}
	if strings.TrimSpace(j.URL) == "" {
		return problem.New(codeInvalid, "jetstream.url must not be empty when bus.type=jetstream")
	}
	if strings.TrimSpace(j.StreamName) == "" {
		return problem.New(codeInvalid, "jetstream.stream_name must not be empty when bus.type=jetstream")
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"jetstream.dedup_window", j.DedupWindow},
		{"jetstream.max_age", j.MaxAge},
		{"jetstream.ack_wait", j.AckWait},
	} {
		d, err := time.ParseDuration(field.value)
		if err != nil || d <= 0 {
			return problem.Newf(codeInvalid, "%s: invalid duration %q", field.name, field.value)
		}
	}
	if strings.TrimSpace(j.ConsumerDurable) == "" {
		return problem.New(codeInvalid, "jetstream.consumer_durable must not be empty when bus.type=jetstream")
	}
	if j.MaxAckPending <= 0 {
		return problem.Newf(codeInvalid, "jetstream.max_ack_pending must be > 0, got %d", j.MaxAckPending)
	}
	if j.MaxDeliver <= 0 {
		return problem.Newf(codeInvalid, "jetstream.max_deliver must be > 0, got %d", j.MaxDeliver)
	}
	switch strings.ToLower(strings.TrimSpace(j.DeliverPolicy)) {
	case "all", "new", "last":
	default:
		return problem.Newf(codeInvalid, "jetstream.deliver_policy must be all|new|last, got %q", j.DeliverPolicy)
	}
	if len(j.FilterSubjects) == 0 {
		return problem.New(codeInvalid, "jetstream.filter_subjects must not be empty when bus.type=jetstream")
	}
	for i, s := range j.FilterSubjects {
		if strings.TrimSpace(s) == "" {
			return problem.Newf(codeInvalid, "jetstream.filter_subjects[%d] must not be empty", i)
		}
	}
	if j.ShardGroupCount < 1 {
		return problem.Newf(codeInvalid, "jetstream.shard_group_count must be >= 1, got %d", j.ShardGroupCount)
	}
	if j.ShardGroupID < 0 || j.ShardGroupID >= j.ShardGroupCount {
		return problem.Newf(codeInvalid, "jetstream.shard_group_id must be in [0, %d), got %d", j.ShardGroupCount, j.ShardGroupID)
	}
	if _, err := parseByteSize(j.MaxBytes); err != nil {
		return problem.Newf(codeInvalid, "jetstream.max_bytes: invalid size %q: %v", j.MaxBytes, err)
	}
	return nil
}

func validateLog(l LogConfig) *problem.Problem {
	switch strings.ToLower(l.Level) {
	case "debug", "info", "warn", "error":
	default:
		return problem.Newf(codeInvalid, "log.level must be one of debug|info|warn|error, got %q", l.Level)
	}
	switch strings.ToLower(l.Format) {
	case "text", "json":
	default:
		return problem.Newf(codeInvalid, "log.format must be text or json, got %q", l.Format)
	}
	return nil
}

func validateHTTP(h HTTPConfig) *problem.Problem {
	if strings.TrimSpace(h.Addr) == "" {
		return problem.New(codeInvalid, "http.addr must not be empty")
	}
	tlsCert := strings.TrimSpace(h.TLSCert)
	tlsKey := strings.TrimSpace(h.TLSKey)
	if (tlsCert == "") != (tlsKey == "") {
		return problem.New(codeInvalid, "http.tls_cert and http.tls_key must be configured together")
	}
	if h.TLSRequired && tlsCert == "" {
		return problem.New(codeInvalid, "http.tls_required is true but http.tls_cert and http.tls_key are not configured")
	}
	var publisherFlushTimeout time.Duration
	var guardianShutdownTimeout time.Duration
	for _, field := range []struct {
		name  string
		value string
	}{
		{"http.read_timeout", h.ReadTimeout},
		{"http.write_timeout", h.WriteTimeout},
		{"http.idle_timeout", h.IdleTimeout},
		{"http.shutdown_timeout", h.ShutdownTimeout},
		{"http.publisher_flush_timeout", h.PublisherFlushTimeout},
		{"http.guardian_shutdown_timeout", h.GuardianShutdownTimeout},
	} {
		d, err := time.ParseDuration(field.value)
		if err != nil {
			return problem.Newf(codeInvalid, "%s: invalid duration %q: %v", field.name, field.value, err)
		}
		switch field.name {
		case "http.publisher_flush_timeout":
			publisherFlushTimeout = d
		case "http.guardian_shutdown_timeout":
			guardianShutdownTimeout = d
		}
	}
	if publisherFlushTimeout >= guardianShutdownTimeout {
		return problem.Newf(
			codeInvalid,
			"http.publisher_flush_timeout (%s) must be less than http.guardian_shutdown_timeout (%s)",
			publisherFlushTimeout,
			guardianShutdownTimeout,
		)
	}
	return nil
}

func validateWS(w WSConfig) *problem.Problem {
	if w.RateLimit.MaxPerSecond < 0 {
		return problem.Newf(codeInvalid, "ws.rate_limit.max_per_second must be >= 0, got %d", w.RateLimit.MaxPerSecond)
	}
	if w.RateLimit.BurstCapacity < 0 {
		return problem.Newf(codeInvalid, "ws.rate_limit.burst_capacity must be >= 0, got %d", w.RateLimit.BurstCapacity)
	}
	if w.Auth.Enabled {
		if len(w.Auth.APIKeys) == 0 {
			return problem.New(codeInvalid, "ws.auth.api_keys must not be empty when ws.auth.enabled=true")
		}
		for key, clientID := range w.Auth.APIKeys {
			if strings.TrimSpace(key) == "" {
				return problem.New(codeInvalid, "ws.auth.api_keys keys must not be empty")
			}
			if strings.TrimSpace(clientID) == "" {
				return problem.Newf(codeInvalid, "ws.auth.api_keys[%q] client_id must not be empty", key)
			}
		}
	}
	if w.RateLimit.Enabled {
		if w.RateLimit.MaxPerSecond <= 0 {
			return problem.Newf(codeInvalid, "ws.rate_limit.max_per_second must be > 0 when ws.rate_limit.enabled=true, got %d", w.RateLimit.MaxPerSecond)
		}
		if w.RateLimit.BurstCapacity <= 0 {
			return problem.Newf(codeInvalid, "ws.rate_limit.burst_capacity must be > 0 when ws.rate_limit.enabled=true, got %d", w.RateLimit.BurstCapacity)
		}
	}
	return nil
}

func validateDelivery(d DeliveryConfig) *problem.Problem {
	if d.MaxSessions < 0 {
		return problem.Newf(codeInvalid, "delivery.max_sessions must be >= 0, got %d", d.MaxSessions)
	}
	if d.SessionOutboundQueueSize <= 0 {
		return problem.Newf(codeInvalid, "delivery.session_outbound_queue_size must be > 0, got %d", d.SessionOutboundQueueSize)
	}
	if d.SlowClientDropThreshold < 0 {
		return problem.Newf(codeInvalid, "delivery.slow_client_drop_threshold must be >= 0, got %d", d.SlowClientDropThreshold)
	}
	switch strings.ToLower(strings.TrimSpace(d.BackpressurePolicy)) {
	case "drop_newest", "drop_oldest", "priority_drop":
	default:
		return problem.Newf(codeInvalid, "delivery.backpressure_policy must be drop_newest|drop_oldest|priority_drop, got %q", d.BackpressurePolicy)
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"delivery.router_ready_timeout", d.RouterReadyTimeout},
		{"delivery.subsystem_ready_timeout", d.SubsystemReadyTimeout},
		{"delivery.session_spawn_timeout", d.SessionSpawnTimeout},
	} {
		if _, err := time.ParseDuration(field.value); err != nil {
			return problem.Newf(codeInvalid, "%s: invalid duration %q: %v", field.name, field.value, err)
		}
	}
	if !d.Enabled {
		return nil
	}
	if strings.TrimSpace(d.NATS.ConsumerDurable) == "" {
		return problem.New(codeInvalid, "delivery.nats.consumer_durable must not be empty when delivery.enabled=true")
	}
	if len(d.NATS.FilterSubjects) == 0 {
		return problem.New(codeInvalid, "delivery.nats.filter_subjects must not be empty when delivery.enabled=true")
	}
	for i, subject := range d.NATS.FilterSubjects {
		if strings.TrimSpace(subject) == "" {
			return problem.Newf(codeInvalid, "delivery.nats.filter_subjects[%d] must not be empty", i)
		}
	}
	return nil
}

func validateConsumer(c ConsumerConfig) *problem.Problem {
	exchanges := c.Exchanges
	if len(exchanges) == 0 {
		exchanges = []ConsumerExchangeConfig{synthesizeLegacyExchange(c)}
	}
	if prob := validateConsumerExchanges(exchanges); prob != nil {
		return prob
	}

	if c.StreamsPerTicker <= 0 {
		return problem.Newf(codeInvalid, "consumer.streams_per_ticker must be > 0, got %d", c.StreamsPerTicker)
	}
	if c.MaxStreamsPerWebsocket <= 0 {
		return problem.Newf(codeInvalid, "consumer.max_streams_per_websocket must be > 0, got %d", c.MaxStreamsPerWebsocket)
	}
	if c.MaxWebsockets <= 0 {
		return problem.Newf(codeInvalid, "consumer.max_websockets must be > 0, got %d", c.MaxWebsockets)
	}
	if c.BackpressureBufferSize <= 0 {
		return problem.Newf(codeInvalid, "consumer.backpressure_buffer_size must be > 0, got %d", c.BackpressureBufferSize)
	}
	switch strings.TrimSpace(c.BackpressurePolicy) {
	case "drop_oldest", "drop_depth_keep_trades":
	default:
		return problem.Newf(codeInvalid, "consumer.backpressure_policy must be drop_oldest|drop_depth_keep_trades, got %q", c.BackpressurePolicy)
	}
	if c.ReconnectJitter < 0 || c.ReconnectJitter > 1 {
		return problem.Newf(codeInvalid, "consumer.reconnect_jitter must be in [0,1], got %f", c.ReconnectJitter)
	}
	if c.ReconnectRetryBudget <= 0 {
		return problem.Newf(codeInvalid, "consumer.reconnect_retry_budget must be > 0, got %d", c.ReconnectRetryBudget)
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"consumer.max_websocket_lifetime", c.MaxWebsocketLifetime},
		{"consumer.respawn_overlap", c.RespawnOverlap},
		{"consumer.reconnect_base_backoff", c.ReconnectBaseBackoff},
		{"consumer.reconnect_max_backoff", c.ReconnectMaxBackoff},
		{"consumer.reconnect_budget_window", c.ReconnectBudgetWindow},
		{"consumer.reconnect_cooldown", c.ReconnectCooldown},
	} {
		if _, err := time.ParseDuration(field.value); err != nil {
			return problem.Newf(codeInvalid, "%s: invalid duration %q: %v", field.name, field.value, err)
		}
	}

	for _, ex := range exchanges {
		if !strings.EqualFold(ex.MarketType, "SPOT") {
			continue
		}
		// coinbase/hyperliquid override StreamsPerTicker in their runtime
		// builders; the global value does not apply to them.
		typ := strings.ToLower(strings.TrimSpace(ex.Type))
		if typ != "binance" && typ != "bybit" {
			continue
		}
		requiredStreams := int64(2)
		if c.EnableMarkPriceLiquidation {
			requiredStreams = 4
		}
		if c.StreamsPerTicker != requiredStreams {
			return problem.Newf(
				codeInvalid,
				"consumer.streams_per_ticker=%d incompatible with spot runtime baseline=%d (enable_markprice_liquidation=%t)",
				c.StreamsPerTicker,
				requiredStreams,
				c.EnableMarkPriceLiquidation,
			)
		}
	}
	return nil
}

func validateConsumerExchanges(exchanges []ConsumerExchangeConfig) *problem.Problem {
	seen := make(map[string]struct{}, len(exchanges))
	for i, ex := range exchanges {
		name := strings.ToLower(strings.TrimSpace(ex.Name))
		if name == "" {
			return problem.Newf(codeInvalid, "consumer.exchanges[%d].name must not be empty", i)
		}
		if !metricsExchangeNamePattern.MatchString(name) {
			return problem.Newf(
				codeInvalid,
				`consumer.exchanges[%d].name must match [a-z0-9_-]{1,24} for metrics labels, got %q`,
				i,
				ex.Name,
			)
		}
		if _, exists := seen[name]; exists {
			return problem.Newf(codeInvalid, "consumer.exchanges name must be unique, duplicate %q", ex.Name)
		}
		seen[name] = struct{}{}

		typ := strings.ToLower(strings.TrimSpace(ex.Type))
		switch typ {
		case "binance", "bybit", "coinbase", "hyperliquid", "kraken", "krakenf":
		default:
			return problem.Newf(codeInvalid, "consumer.exchanges[%d].type must be binance|bybit|coinbase|hyperliquid|kraken|krakenf, got %q", i, ex.Type)
		}

		if len(ex.Tickers) == 0 {
			return problem.Newf(codeInvalid, "consumer.exchanges[%d].tickers must not be empty", i)
		}
		for j, t := range ex.Tickers {
			if strings.TrimSpace(t) == "" {
				return problem.Newf(codeInvalid, "consumer.exchanges[%d].tickers[%d] must not be empty", i, j)
			}
		}

		switch strings.ToUpper(strings.TrimSpace(ex.MarketType)) {
		case "SPOT", "USD_M_FUTURES", "COIN_M_FUTURES":
		default:
			return problem.Newf(codeInvalid, "consumer.exchanges[%d].market_type must be SPOT|USD_M_FUTURES|COIN_M_FUTURES, got %q", i, ex.MarketType)
		}

		if strings.TrimSpace(ex.BaseURL) == "" {
			return problem.Newf(codeInvalid, "consumer.exchanges[%d].base_url must not be empty", i)
		}
	}
	return nil
}

func validateMarketData(m MarketDataConfig) *problem.Problem {
	if _, p := envelope.NormalizeContentType(m.PublishContentType); p != nil {
		return problem.Newf(codeInvalid, "marketdata.publish_content_type must be application/json|application/protobuf, got %q", m.PublishContentType)
	}
	if m.MaxInstruments <= 0 {
		return problem.Newf(codeInvalid, "marketdata.max_instruments must be > 0, got %d", m.MaxInstruments)
	}
	if strings.TrimSpace(m.RecordPath) == "." {
		return problem.New(codeInvalid, "marketdata.record_path must not be \".\"")
	}
	if strings.TrimSpace(m.ReplayPath) == "." {
		return problem.New(codeInvalid, "marketdata.replay_path must not be \".\"")
	}
	return nil
}

func validateReplay(bus BusConfig, marketData MarketDataConfig, replay ReplayConfig) *problem.Problem {
	mode := strings.ToLower(strings.TrimSpace(replay.Mode))
	switch mode {
	case "off", "file", "jetstream":
	default:
		return problem.Newf(codeInvalid, "replay.mode must be off|file|jetstream, got %q", replay.Mode)
	}

	switch strings.ToLower(strings.TrimSpace(replay.OnDecodeError)) {
	case "fail", "skip":
	default:
		return problem.Newf(codeInvalid, "replay.on_decode_error must be fail|skip, got %q", replay.OnDecodeError)
	}

	if mode == "file" && strings.TrimSpace(marketData.ReplayPath) == "" {
		return problem.New(codeInvalid, "marketdata.replay_path must not be empty when replay.mode=file")
	}
	if mode != "jetstream" {
		return nil
	}

	if !strings.EqualFold(strings.TrimSpace(bus.Type), "jetstream") {
		return problem.New(codeInvalid, "replay.mode=jetstream requires bus.type=jetstream")
	}

	j := replay.JetStream
	if strings.TrimSpace(j.SubjectFilter) == "" {
		return problem.New(codeInvalid, "replay.jetstream.subject_filter must not be empty when replay.mode=jetstream")
	}
	if j.MaxMessages < 1 || j.MaxMessages > 10_000_000 {
		return problem.Newf(codeInvalid, "replay.jetstream.max_messages must be in [1,10000000], got %d", j.MaxMessages)
	}
	switch strings.ToLower(strings.TrimSpace(j.DeliverPolicy)) {
	case "all", "by_start_time":
	default:
		return problem.Newf(codeInvalid, "replay.jetstream.deliver_policy must be all|by_start_time, got %q", j.DeliverPolicy)
	}

	if strings.EqualFold(strings.TrimSpace(j.DeliverPolicy), "by_start_time") {
		d, err := time.ParseDuration(j.Window)
		if err != nil || d <= 0 {
			return problem.Newf(codeInvalid, "replay.jetstream.window must be > 0 duration when deliver_policy=by_start_time, got %q", j.Window)
		}
	}
	if strings.TrimSpace(j.Window) != "" {
		if d, err := time.ParseDuration(j.Window); err != nil || d <= 0 {
			return problem.Newf(codeInvalid, "replay.jetstream.window: invalid duration %q", j.Window)
		}
	}
	if j.MergeBuffer <= 0 {
		return problem.Newf(codeInvalid, "replay.jetstream.merge_buffer must be > 0, got %d", j.MergeBuffer)
	}

	return nil
}

func validateProcessor(p ProcessorConfig) *problem.Problem {
	if d, err := time.ParseDuration(p.PublisherTimeout); err != nil || d <= 0 {
		return problem.Newf(codeInvalid, "processor.publisher_timeout must be > 0 duration, got %q", p.PublisherTimeout)
	}
	if p.BusCapacity <= 0 {
		return problem.Newf(codeInvalid, "processor.bus_capacity must be > 0, got %d", p.BusCapacity)
	}
	if p.MaxInstruments <= 0 {
		return problem.Newf(codeInvalid, "processor.max_instruments must be > 0, got %d", p.MaxInstruments)
	}
	if p.Candle.MaxCandles <= 0 {
		return problem.Newf(codeInvalid, "processor.candle.max_candles must be > 0, got %d", p.Candle.MaxCandles)
	}
	if p.Stats.MaxWindows <= 0 {
		return problem.Newf(codeInvalid, "processor.stats.max_windows must be > 0, got %d", p.Stats.MaxWindows)
	}
	if p.RTPublish.OrderbookIntervalMs < 0 {
		return problem.Newf(codeInvalid, "processor.rt_publish.orderbook_interval_ms must be >= 0, got %d", p.RTPublish.OrderbookIntervalMs)
	}
	if p.RTPublish.HeatmapIntervalMs < 0 {
		return problem.Newf(codeInvalid, "processor.rt_publish.heatmap_interval_ms must be >= 0, got %d", p.RTPublish.HeatmapIntervalMs)
	}
	if p.RTPublish.VolumeIntervalMs < 0 {
		return problem.Newf(codeInvalid, "processor.rt_publish.volume_interval_ms must be >= 0, got %d", p.RTPublish.VolumeIntervalMs)
	}
	if p.CatchUpSkipBookDeltaSkewMs < 0 {
		return problem.Newf(codeInvalid, "processor.catchup_skip_bookdelta_skew_ms must be >= 0, got %d", p.CatchUpSkipBookDeltaSkewMs)
	}
	if p.CatchUpSkipTradeSkewMs < 0 {
		return problem.Newf(codeInvalid, "processor.catchup_skip_trade_skew_ms must be >= 0, got %d", p.CatchUpSkipTradeSkewMs)
	}
	if p.CatchUpSkipStatsSkewMs < 0 {
		return problem.Newf(codeInvalid, "processor.catchup_skip_stats_skew_ms must be >= 0, got %d", p.CatchUpSkipStatsSkewMs)
	}
	for i, venue := range p.SubMinuteRollout.Venues {
		if strings.TrimSpace(venue) == "" {
			return problem.Newf(codeInvalid, "processor.subminute_rollout.venues[%d] must not be empty", i)
		}
	}
	for i, instrument := range p.SubMinuteRollout.Instruments {
		if strings.TrimSpace(instrument) == "" {
			return problem.Newf(codeInvalid, "processor.subminute_rollout.instruments[%d] must not be empty", i)
		}
	}

	insights := p.Insights
	if strings.TrimSpace(insights.JoinTradesSubject) == "" {
		return problem.New(codeInvalid, "processor.insights.join_trades_subject must not be empty")
	}
	if insights.MaxInstruments <= 0 {
		return problem.Newf(codeInvalid, "processor.insights.max_instruments must be > 0, got %d", insights.MaxInstruments)
	}
	ttl, err := time.ParseDuration(insights.TTL)
	if err != nil || ttl <= 0 {
		return problem.Newf(codeInvalid, "processor.insights.ttl must be > 0 duration, got %q", insights.TTL)
	}
	if insights.SweepEveryN < 0 {
		return problem.Newf(codeInvalid, "processor.insights.sweep_every_n must be >= 0, got %d", insights.SweepEveryN)
	}
	if strings.TrimSpace(insights.SweepEvery) != "" {
		d, err := time.ParseDuration(insights.SweepEvery)
		if err != nil || d < 0 {
			return problem.Newf(codeInvalid, "processor.insights.sweep_every must be >= 0 duration, got %q", insights.SweepEvery)
		}
	}
	if insights.MinVenues < 2 {
		return problem.Newf(codeInvalid, "processor.insights.min_venues must be >= 2, got %d", insights.MinVenues)
	}
	if insights.MinSpreadBPS < 0 || math.IsNaN(insights.MinSpreadBPS) || math.IsInf(insights.MinSpreadBPS, 0) {
		return problem.Newf(codeInvalid, "processor.insights.min_spread_bps must be a finite number >= 0, got %v", insights.MinSpreadBPS)
	}
	switch strings.ToLower(strings.TrimSpace(insights.RoundingMode)) {
	case "", "half_even", "floor":
	default:
		return problem.Newf(codeInvalid, "processor.insights.rounding_mode must be half_even|floor, got %q", insights.RoundingMode)
	}
	return nil
}

func validateStore(s StoreConfig) *problem.Problem {
	if strings.TrimSpace(s.ClickHouse.DSN) == "" {
		return problem.New(codeInvalid, "store.clickhouse.dsn must not be empty")
	}
	if s.Batch.MaxRows <= 0 {
		return problem.New(codeInvalid, "store.batch.max_rows must be > 0")
	}
	if s.Batch.MaxBytes < 0 {
		return problem.New(codeInvalid, "store.batch.max_bytes must be >= 0")
	}
	if _, err := time.ParseDuration(s.Batch.FlushInterval); err != nil {
		return problem.Newf(codeInvalid, "store.batch.flush_interval invalid: %v", err)
	}
	return nil
}

func validateStorage(s StorageConfig) *problem.Problem {
	if s.Timescale.Enabled {
		if strings.TrimSpace(s.Timescale.DSN) == "" {
			return problem.New(codeInvalid, "storage.timescale.dsn must not be empty when storage.timescale.enabled=true")
		}
		if s.Timescale.MaxConns <= 0 {
			return problem.Newf(codeInvalid, "storage.timescale.max_conns must be > 0, got %d", s.Timescale.MaxConns)
		}
		if s.Timescale.MinConns < 0 {
			return problem.Newf(codeInvalid, "storage.timescale.min_conns must be >= 0, got %d", s.Timescale.MinConns)
		}
		if s.Timescale.MinConns > s.Timescale.MaxConns {
			return problem.Newf(codeInvalid, "storage.timescale.min_conns (%d) must be <= max_conns (%d)", s.Timescale.MinConns, s.Timescale.MaxConns)
		}
		for _, field := range []struct {
			name  string
			value string
		}{
			{"storage.timescale.max_conn_lifetime", s.Timescale.MaxConnLifetime},
			{"storage.timescale.max_conn_idle_time", s.Timescale.MaxConnIdleTime},
			{"storage.timescale.health_check_period", s.Timescale.HealthCheckPeriod},
		} {
			if _, err := time.ParseDuration(field.value); err != nil {
				return problem.Newf(codeInvalid, "%s: invalid duration %q: %v", field.name, field.value, err)
			}
		}
	}

	if s.ClickHouse.Enabled {
		if len(s.ClickHouse.Addrs) == 0 {
			return problem.New(codeInvalid, "storage.clickhouse.addrs must not be empty when storage.clickhouse.enabled=true")
		}
		for i, addr := range s.ClickHouse.Addrs {
			if strings.TrimSpace(addr) == "" {
				return problem.Newf(codeInvalid, "storage.clickhouse.addrs[%d] must not be empty", i)
			}
		}
		if strings.TrimSpace(s.ClickHouse.Database) == "" {
			return problem.New(codeInvalid, "storage.clickhouse.database must not be empty when storage.clickhouse.enabled=true")
		}
		if s.ClickHouse.MaxOpenConns <= 0 {
			return problem.Newf(codeInvalid, "storage.clickhouse.max_open_conns must be > 0, got %d", s.ClickHouse.MaxOpenConns)
		}
		if s.ClickHouse.MaxIdleConns < 0 {
			return problem.Newf(codeInvalid, "storage.clickhouse.max_idle_conns must be >= 0, got %d", s.ClickHouse.MaxIdleConns)
		}
		if s.ClickHouse.MaxIdleConns > s.ClickHouse.MaxOpenConns {
			return problem.Newf(codeInvalid, "storage.clickhouse.max_idle_conns (%d) must be <= max_open_conns (%d)", s.ClickHouse.MaxIdleConns, s.ClickHouse.MaxOpenConns)
		}
		for _, field := range []struct {
			name  string
			value string
		}{
			{"storage.clickhouse.conn_max_lifetime", s.ClickHouse.ConnMaxLifetime},
			{"storage.clickhouse.dial_timeout", s.ClickHouse.DialTimeout},
			{"storage.clickhouse.read_timeout", s.ClickHouse.ReadTimeout},
		} {
			if _, err := time.ParseDuration(field.value); err != nil {
				return problem.Newf(codeInvalid, "%s: invalid duration %q: %v", field.name, field.value, err)
			}
		}
	}

	return nil
}

func validateCrossField(a AppConfig) *problem.Problem {
	if a.Delivery.Enabled && a.Processor.BusCapacity < a.Delivery.SessionOutboundQueueSize {
		return problem.Newf(
			codeInvalid,
			"processor.bus_capacity (%d) must be >= delivery.session_outbound_queue_size (%d) to avoid immediate drops",
			a.Processor.BusCapacity,
			a.Delivery.SessionOutboundQueueSize,
		)
	}
	return nil
}

func runtimeInputSubjectPatterns(cfg AppConfig) ([]string, string) {
	if strings.EqualFold(strings.TrimSpace(cfg.Replay.Mode), "jetstream") {
		filter := strings.TrimSpace(cfg.Replay.JetStream.SubjectFilter)
		if filter == "" {
			return nil, "replay.jetstream.subject_filter"
		}
		return []string{filter}, "replay.jetstream.subject_filter"
	}
	if !strings.EqualFold(strings.TrimSpace(cfg.Bus.Type), "jetstream") {
		return nil, ""
	}

	patterns := make([]string, 0, len(cfg.JetStream.FilterSubjects)+1)
	for _, raw := range cfg.JetStream.FilterSubjects {
		pattern := strings.TrimSpace(raw)
		if pattern != "" {
			patterns = append(patterns, pattern)
		}
	}
	if cfg.Processor.Insights.EnableCrossVenueJoin {
		if join := strings.TrimSpace(cfg.Processor.Insights.JoinTradesSubject); join != "" {
			patterns = append(patterns, join)
		}
	}
	if len(patterns) == 0 {
		return nil, "jetstream.filter_subjects"
	}
	return dedupeStrings(patterns), "jetstream.filter_subjects + processor.insights.join_trades_subject"
}

func expectedTradeSubjects(c ConsumerConfig) []string {
	venues := configuredExchangeVenues(c)
	instruments := []string{"BTCUSDT", "ETHUSDT"}
	out := make([]string, 0, len(venues)*len(instruments))
	for _, venue := range venues {
		for _, instrument := range instruments {
			out = append(out, fmt.Sprintf("marketdata.trade.v1.%s.%s", venue, instrument))
		}
	}
	return out
}

func expectedMarketDataSubjects(c ConsumerConfig) []string {
	venues := configuredExchangeVenues(c)
	out := make([]string, 0, len(venues)*2)
	for _, venue := range venues {
		out = append(out, fmt.Sprintf("marketdata.bookdelta.v1.%s.BTCUSDT", venue))
		out = append(out, fmt.Sprintf("marketdata.trade.v1.%s.BTCUSDT", venue))
	}
	return out
}

func configuredExchangeVenues(c ConsumerConfig) []string {
	exchanges := c.Exchanges
	if len(exchanges) == 0 {
		exchanges = []ConsumerExchangeConfig{synthesizeLegacyExchange(c)}
	}
	seen := make(map[string]struct{}, len(exchanges))
	out := make([]string, 0, len(exchanges))
	for _, ex := range exchanges {
		venue := strings.ToLower(strings.TrimSpace(ex.Type))
		if venue == "" {
			venue = strings.ToLower(strings.TrimSpace(ex.Name))
		}
		if venue == "" {
			continue
		}
		if _, exists := seen[venue]; exists {
			continue
		}
		seen[venue] = struct{}{}
		out = append(out, venue)
	}
	if len(out) == 0 {
		return []string{"binance"}
	}
	sort.Strings(out)
	return out
}

func validateInsightsPublishSubjectPrefix(prefix string) *problem.Problem {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil
	}
	if !strings.HasPrefix(prefix, "insights.") {
		return problem.New(codeInvalid, `processor.insights.snapshot_subject_prefix must start with insights.; e.g. "insights.crossvenue.trade_snapshot.v1"`)
	}
	if !isValidNATSPublishSubject(prefix) {
		return problem.New(codeInvalid, `processor.insights.snapshot_subject_prefix is invalid; e.g. "insights.crossvenue.trade_snapshot.v1"`)
	}
	return nil
}

func isValidNATSSubjectPattern(pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	tokens := strings.Split(pattern, ".")
	for i, token := range tokens {
		if token == "" || strings.ContainsAny(token, " \t\r\n") {
			return false
		}
		if token == ">" {
			return i == len(tokens)-1
		}
		if strings.Contains(token, ">") {
			return false
		}
		if token == "*" {
			continue
		}
		if strings.Contains(token, "*") {
			return false
		}
	}
	return true
}

func isValidNATSPublishSubject(subject string) bool {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return false
	}
	tokens := strings.Split(subject, ".")
	for _, token := range tokens {
		if token == "" || strings.ContainsAny(token, " \t\r\n") {
			return false
		}
		if strings.ContainsAny(token, "*>") {
			return false
		}
	}
	return true
}

func matchesAnySubject(pattern string, subjects []string) bool {
	for _, subject := range subjects {
		if subjectPatternMatches(pattern, subject) {
			return true
		}
	}
	return false
}

func anyPatternMatchesSubject(patterns []string, subject string) bool {
	for _, pattern := range patterns {
		if subjectPatternMatches(pattern, subject) {
			return true
		}
	}
	return false
}

func subjectPatternMatches(pattern, subject string) bool {
	pattern = strings.TrimSpace(pattern)
	subject = strings.TrimSpace(subject)
	if pattern == "" || subject == "" {
		return false
	}
	pTokens := strings.Split(pattern, ".")
	sTokens := strings.Split(subject, ".")

	i, j := 0, 0
	for i < len(pTokens) {
		token := pTokens[i]
		if token == ">" {
			return i == len(pTokens)-1
		}
		if j >= len(sTokens) {
			return false
		}
		if token == "*" {
			i++
			j++
			continue
		}
		if token != sTokens[j] {
			return false
		}
		i++
		j++
	}
	return j == len(sTokens)
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		if _, exists := seen[v]; exists {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// applyDefaults fills zero-value fields with safe defaults.
// It is idempotent: calling it multiple times has no additional effect.
func applyDefaults(c *AppConfig) {
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	if c.Log.Format == "" {
		c.Log.Format = "text"
	}
	if c.HTTP.Addr == "" {
		c.HTTP.Addr = ":8080"
	}
	if c.HTTP.ReadTimeout == "" {
		c.HTTP.ReadTimeout = "10s"
	}
	if c.HTTP.WriteTimeout == "" {
		c.HTTP.WriteTimeout = "15s"
	}
	if c.HTTP.IdleTimeout == "" {
		c.HTTP.IdleTimeout = "60s"
	}
	if c.HTTP.ShutdownTimeout == "" {
		c.HTTP.ShutdownTimeout = "10s"
	}
	if c.HTTP.PublisherFlushTimeout == "" {
		c.HTTP.PublisherFlushTimeout = "3s"
	}
	if c.HTTP.GuardianShutdownTimeout == "" {
		// Backward compatible fallback to legacy shutdown_timeout when the new field is absent.
		c.HTTP.GuardianShutdownTimeout = c.HTTP.ShutdownTimeout
	}
	c.HTTP.TLSCert = strings.TrimSpace(c.HTTP.TLSCert)
	c.HTTP.TLSKey = strings.TrimSpace(c.HTTP.TLSKey)
	if c.Delivery.MaxSessions == 0 {
		c.Delivery.MaxSessions = 10000
	}
	if c.Delivery.SessionOutboundQueueSize == 0 {
		c.Delivery.SessionOutboundQueueSize = 512
	}
	if strings.TrimSpace(c.Delivery.BackpressurePolicy) == "" {
		c.Delivery.BackpressurePolicy = "drop_newest"
	}
	if c.Delivery.SlowClientDropThreshold == 0 {
		c.Delivery.SlowClientDropThreshold = 1000
	}
	if c.Delivery.RouterReadyTimeout == "" {
		c.Delivery.RouterReadyTimeout = "2s"
	}
	if c.Delivery.SubsystemReadyTimeout == "" {
		c.Delivery.SubsystemReadyTimeout = "500ms"
	}
	if c.Delivery.SessionSpawnTimeout == "" {
		c.Delivery.SessionSpawnTimeout = "2s"
	}
	if strings.TrimSpace(c.Delivery.NATS.ConsumerDurable) == "" {
		c.Delivery.NATS.ConsumerDurable = "delivery-v1"
	}
	if len(c.Delivery.NATS.FilterSubjects) == 0 {
		c.Delivery.NATS.FilterSubjects = []string{"marketdata.>", "aggregation.>", "insights.>"}
	}
	if c.Shard.Count == 0 {
		c.Shard.Count = 1
	}
	// Shard.Index zero value (0) is the correct default.
	// Shard.Registry.Enabled zero value (false) is the correct default.
	// Shard.Registry.Strict zero value (false) is the correct default.
	if c.Shard.Registry.TopologyGrace == "" {
		c.Shard.Registry.TopologyGrace = "60s"
	}
	if c.Bus.Type == "" {
		c.Bus.Type = "inmemory"
	}
	if c.Bus.WireFormat == "" {
		c.Bus.WireFormat = "proto"
	}
	c.Bus.WireFormat = strings.ToLower(strings.TrimSpace(c.Bus.WireFormat))
	if c.JetStream.URL == "" {
		c.JetStream.URL = "nats://localhost:4222"
	}
	if c.JetStream.StreamName == "" {
		c.JetStream.StreamName = "MARKETDATA"
	}
	if c.JetStream.ConsumerDurable == "" {
		c.JetStream.ConsumerDurable = "processor-v1"
	}
	if c.JetStream.AckWait == "" {
		c.JetStream.AckWait = "30s"
	}
	if c.JetStream.MaxAckPending == 0 {
		c.JetStream.MaxAckPending = 1024
	}
	if c.JetStream.MaxDeliver == 0 {
		c.JetStream.MaxDeliver = 10
	}
	if c.JetStream.DeliverPolicy == "" {
		c.JetStream.DeliverPolicy = "all"
	}
	if len(c.JetStream.FilterSubjects) == 0 {
		c.JetStream.FilterSubjects = []string{"marketdata.>"}
	}
	if c.JetStream.ShardGroupCount == 0 {
		c.JetStream.ShardGroupCount = 1
	}
	// ShardGroupID zero value (0) is the correct default.
	if c.JetStream.DedupWindow == "" {
		c.JetStream.DedupWindow = "5m"
	}
	if c.JetStream.MaxAge == "" {
		c.JetStream.MaxAge = "24h"
	}
	if c.JetStream.MaxBytes == "" {
		c.JetStream.MaxBytes = "10GB"
	}
	if c.Consumer.Exchange == "" {
		c.Consumer.Exchange = "binance"
	}
	if c.Consumer.MarketType == "" {
		c.Consumer.MarketType = "SPOT"
	}
	if len(c.Consumer.Tickers) == 0 {
		c.Consumer.Tickers = []string{"BTC-USDT", "ETH-USDT"}
	}
	if c.Consumer.BinanceWSBaseURL == "" {
		c.Consumer.BinanceWSBaseURL = "wss://stream.binance.com:9443/stream"
	}
	if c.Consumer.StreamsPerTicker == 0 {
		c.Consumer.StreamsPerTicker = 2
	}
	if c.Consumer.MaxStreamsPerWebsocket == 0 {
		c.Consumer.MaxStreamsPerWebsocket = 200
	}
	if c.Consumer.MaxWebsockets == 0 {
		c.Consumer.MaxWebsockets = 5
	}
	if c.Consumer.MaxWebsocketLifetime == "" {
		c.Consumer.MaxWebsocketLifetime = "45m"
	}
	if c.Consumer.RespawnOverlap == "" {
		c.Consumer.RespawnOverlap = "5s"
	}
	if c.Consumer.BackpressureBufferSize == 0 {
		c.Consumer.BackpressureBufferSize = 8192
	}
	if c.Consumer.BackpressurePolicy == "" {
		c.Consumer.BackpressurePolicy = "drop_depth_keep_trades"
	}
	if c.Consumer.ReconnectBaseBackoff == "" {
		c.Consumer.ReconnectBaseBackoff = "500ms"
	}
	if c.Consumer.ReconnectMaxBackoff == "" {
		c.Consumer.ReconnectMaxBackoff = "30s"
	}
	if c.Consumer.ReconnectJitter == 0 {
		c.Consumer.ReconnectJitter = 0.2
	}
	if c.Consumer.ReconnectRetryBudget == 0 {
		c.Consumer.ReconnectRetryBudget = 20
	}
	if c.Consumer.ReconnectBudgetWindow == "" {
		c.Consumer.ReconnectBudgetWindow = "1m"
	}
	if c.Consumer.ReconnectCooldown == "" {
		c.Consumer.ReconnectCooldown = "30s"
	}
	normalizeConsumerExchanges(&c.Consumer)
	if c.MarketData.PublishContentType == "" {
		c.MarketData.PublishContentType = wireFormatContentType(c.Bus.WireFormat)
	}
	if c.MarketData.MaxInstruments == 0 {
		c.MarketData.MaxInstruments = 2048
	}
	if c.Replay.Mode == "" {
		c.Replay.Mode = "off"
	}
	if c.Replay.OnDecodeError == "" {
		c.Replay.OnDecodeError = "fail"
	}
	if c.Replay.JetStream.SubjectFilter == "" {
		c.Replay.JetStream.SubjectFilter = "marketdata.>"
	}
	if c.Replay.JetStream.MaxMessages == 0 {
		c.Replay.JetStream.MaxMessages = 100_000
	}
	if c.Replay.JetStream.MergeBuffer == 0 {
		c.Replay.JetStream.MergeBuffer = 4096
	}
	if c.Replay.JetStream.DeliverPolicy == "" {
		if strings.TrimSpace(c.Replay.JetStream.Window) != "" {
			c.Replay.JetStream.DeliverPolicy = "by_start_time"
		} else {
			c.Replay.JetStream.DeliverPolicy = "all"
		}
	}

	c.MarketData.RecordPath = strings.TrimSpace(c.MarketData.RecordPath)
	c.MarketData.ReplayPath = strings.TrimSpace(c.MarketData.ReplayPath)
	c.Replay.Mode = strings.TrimSpace(c.Replay.Mode)
	c.Replay.OnDecodeError = strings.TrimSpace(c.Replay.OnDecodeError)
	c.Replay.JetStream.Window = strings.TrimSpace(c.Replay.JetStream.Window)
	c.Replay.JetStream.SubjectFilter = strings.TrimSpace(c.Replay.JetStream.SubjectFilter)
	c.Replay.JetStream.DeliverPolicy = strings.TrimSpace(c.Replay.JetStream.DeliverPolicy)
	if c.Processor.PublisherTimeout == "" {
		c.Processor.PublisherTimeout = "5s"
	}
	if c.Processor.BusCapacity == 0 {
		c.Processor.BusCapacity = 1024
	}
	if c.Processor.MaxInstruments == 0 {
		c.Processor.MaxInstruments = 2048
	}
	if c.Processor.Candle.MaxCandles == 0 {
		c.Processor.Candle.MaxCandles = 50_000
	}
	if c.Processor.Stats.MaxWindows == 0 {
		c.Processor.Stats.MaxWindows = 50_000
	}
	if c.Processor.RTPublish.OrderbookIntervalMs == 0 && !c.Processor.RTPublish.orderbookConfigured() {
		c.Processor.RTPublish.OrderbookIntervalMs = 200
	}
	if c.Processor.RTPublish.HeatmapIntervalMs == 0 && !c.Processor.RTPublish.heatmapConfigured() {
		c.Processor.RTPublish.HeatmapIntervalMs = 200
	}
	if c.Processor.RTPublish.VolumeIntervalMs == 0 && !c.Processor.RTPublish.volumeConfigured() {
		c.Processor.RTPublish.VolumeIntervalMs = 250
	}
	if c.Processor.Insights.JoinTradesSubject == "" {
		c.Processor.Insights.JoinTradesSubject = "marketdata.trade.v1.>"
	}
	if c.Processor.Insights.MaxInstruments == 0 {
		c.Processor.Insights.MaxInstruments = 10_000
	}
	if c.Processor.Insights.TTL == "" {
		c.Processor.Insights.TTL = "1h"
	}
	if c.Processor.Insights.SweepEveryN == 0 && strings.TrimSpace(c.Processor.Insights.SweepEvery) == "" {
		c.Processor.Insights.SweepEveryN = 1024
	}
	if c.Processor.Insights.SweepEvery == "" {
		c.Processor.Insights.SweepEvery = "30s"
	}
	if c.Processor.Insights.MinVenues == 0 {
		c.Processor.Insights.MinVenues = 2
	}
	if c.Processor.Insights.RoundingMode == "" {
		c.Processor.Insights.RoundingMode = "half_even"
	}
	if !c.Processor.SubMinuteRollout.enabledConfigured() {
		c.Processor.SubMinuteRollout.Enabled = true
	}
	c.Processor.SubMinuteRollout.Venues = dedupeStrings(c.Processor.SubMinuteRollout.Venues)
	c.Processor.SubMinuteRollout.Instruments = dedupeStrings(c.Processor.SubMinuteRollout.Instruments)
	if c.Store.ClickHouse.DSN == "" {
		c.Store.ClickHouse.DSN = "clickhouse://default:password@localhost:9000/default"
	}
	if c.Store.Batch.MaxRows <= 0 {
		c.Store.Batch.MaxRows = 1
	}
	if strings.TrimSpace(c.Store.Batch.FlushInterval) == "" {
		c.Store.Batch.FlushInterval = "100ms"
	}
	if c.Storage.Timescale.MaxConns <= 0 {
		c.Storage.Timescale.MaxConns = 10
	}
	if c.Storage.Timescale.MinConns < 0 {
		c.Storage.Timescale.MinConns = 0
	}
	if strings.TrimSpace(c.Storage.Timescale.MaxConnLifetime) == "" {
		c.Storage.Timescale.MaxConnLifetime = "1h"
	}
	if strings.TrimSpace(c.Storage.Timescale.MaxConnIdleTime) == "" {
		c.Storage.Timescale.MaxConnIdleTime = "15m"
	}
	if strings.TrimSpace(c.Storage.Timescale.HealthCheckPeriod) == "" {
		c.Storage.Timescale.HealthCheckPeriod = "30s"
	}
	if len(c.Storage.ClickHouse.Addrs) == 0 {
		c.Storage.ClickHouse.Addrs = []string{"127.0.0.1:9000"}
	}
	if c.Storage.ClickHouse.Database == "" {
		c.Storage.ClickHouse.Database = "default"
	}
	if c.Storage.ClickHouse.Username == "" {
		c.Storage.ClickHouse.Username = "default"
	}
	if c.Storage.ClickHouse.MaxOpenConns <= 0 {
		c.Storage.ClickHouse.MaxOpenConns = 10
	}
	if c.Storage.ClickHouse.MaxIdleConns < 0 {
		c.Storage.ClickHouse.MaxIdleConns = 0
	}
	if strings.TrimSpace(c.Storage.ClickHouse.ConnMaxLifetime) == "" {
		c.Storage.ClickHouse.ConnMaxLifetime = "1h"
	}
	if strings.TrimSpace(c.Storage.ClickHouse.DialTimeout) == "" {
		c.Storage.ClickHouse.DialTimeout = "2s"
	}
	if strings.TrimSpace(c.Storage.ClickHouse.ReadTimeout) == "" {
		c.Storage.ClickHouse.ReadTimeout = "5s"
	}
	if c.WS.RateLimit.MaxPerSecond <= 0 {
		c.WS.RateLimit.MaxPerSecond = 100
	}
	if c.WS.RateLimit.BurstCapacity <= 0 {
		c.WS.RateLimit.BurstCapacity = 200
	}
	if len(c.WS.Auth.APIKeys) > 0 {
		normalized := make(map[string]string, len(c.WS.Auth.APIKeys))
		for key, clientID := range c.WS.Auth.APIKeys {
			k := strings.TrimSpace(key)
			v := strings.TrimSpace(clientID)
			if k == "" || v == "" {
				continue
			}
			normalized[k] = v
		}
		c.WS.Auth.APIKeys = normalized
	}
	c.Processor.Insights.JoinTradesSubject = strings.TrimSpace(c.Processor.Insights.JoinTradesSubject)
	c.Processor.Insights.SnapshotSubjectPrefix = strings.TrimSpace(c.Processor.Insights.SnapshotSubjectPrefix)
	c.Processor.Insights.TTL = strings.TrimSpace(c.Processor.Insights.TTL)
	c.Processor.Insights.SweepEvery = strings.TrimSpace(c.Processor.Insights.SweepEvery)
	c.Processor.Insights.RoundingMode = strings.ToLower(strings.TrimSpace(c.Processor.Insights.RoundingMode))
	c.Storage.Timescale.DSN = strings.TrimSpace(c.Storage.Timescale.DSN)
	c.Storage.Timescale.MaxConnLifetime = strings.TrimSpace(c.Storage.Timescale.MaxConnLifetime)
	c.Storage.Timescale.MaxConnIdleTime = strings.TrimSpace(c.Storage.Timescale.MaxConnIdleTime)
	c.Storage.Timescale.HealthCheckPeriod = strings.TrimSpace(c.Storage.Timescale.HealthCheckPeriod)
	for i := range c.Storage.ClickHouse.Addrs {
		c.Storage.ClickHouse.Addrs[i] = strings.TrimSpace(c.Storage.ClickHouse.Addrs[i])
	}
	c.Storage.ClickHouse.Database = strings.TrimSpace(c.Storage.ClickHouse.Database)
	c.Storage.ClickHouse.Username = strings.TrimSpace(c.Storage.ClickHouse.Username)
	c.Storage.ClickHouse.Password = strings.TrimSpace(c.Storage.ClickHouse.Password)
	c.Storage.ClickHouse.ConnMaxLifetime = strings.TrimSpace(c.Storage.ClickHouse.ConnMaxLifetime)
	c.Storage.ClickHouse.DialTimeout = strings.TrimSpace(c.Storage.ClickHouse.DialTimeout)
	c.Storage.ClickHouse.ReadTimeout = strings.TrimSpace(c.Storage.ClickHouse.ReadTimeout)
}

func wireFormatContentType(wireFormat string) string {
	switch strings.ToLower(strings.TrimSpace(wireFormat)) {
	case "proto":
		return envelope.ContentTypeProto
	default:
		return envelope.ContentTypeJSON
	}
}

func normalizeConsumerExchanges(c *ConsumerConfig) {
	c.Exchange = strings.TrimSpace(c.Exchange)
	c.MarketType = strings.ToUpper(strings.TrimSpace(c.MarketType))
	c.BinanceWSBaseURL = strings.TrimSpace(c.BinanceWSBaseURL)

	if len(c.Exchanges) == 0 {
		c.Exchanges = []ConsumerExchangeConfig{synthesizeLegacyExchange(*c)}
	}

	normalized := make([]ConsumerExchangeConfig, 0, len(c.Exchanges))
	for _, raw := range c.Exchanges {
		ex := raw
		ex.Name = strings.ToLower(strings.TrimSpace(ex.Name))
		ex.Type = strings.ToLower(strings.TrimSpace(ex.Type))
		if ex.Name == "" {
			ex.Name = ex.Type
		}
		if ex.Type == "" {
			ex.Type = ex.Name
		}
		if ex.MarketType == "" {
			ex.MarketType = c.MarketType
		}
		ex.MarketType = strings.ToUpper(strings.TrimSpace(ex.MarketType))
		ex.BaseURL = strings.TrimSpace(ex.BaseURL)
		if ex.BaseURL == "" {
			ex.BaseURL = defaultExchangeBaseURL(ex.Type, ex.MarketType, c.BinanceWSBaseURL)
		}

		if len(ex.Tickers) > 0 {
			tickers := make([]string, 0, len(ex.Tickers))
			for _, ticker := range ex.Tickers {
				tickers = append(tickers, strings.TrimSpace(ticker))
			}
			ex.Tickers = tickers
		}
		if len(ex.Buckets) > 0 {
			buckets := make([][]string, 0, len(ex.Buckets))
			for _, bucket := range ex.Buckets {
				tickers := make([]string, 0, len(bucket))
				for _, ticker := range bucket {
					tickers = append(tickers, strings.TrimSpace(ticker))
				}
				buckets = append(buckets, tickers)
			}
			ex.Buckets = buckets
		}
		normalized = append(normalized, ex)
	}

	sort.SliceStable(normalized, func(i, j int) bool {
		if normalized[i].Name != normalized[j].Name {
			return normalized[i].Name < normalized[j].Name
		}
		if normalized[i].Type != normalized[j].Type {
			return normalized[i].Type < normalized[j].Type
		}
		return normalized[i].MarketType < normalized[j].MarketType
	})
	c.Exchanges = normalized
}

func synthesizeLegacyExchange(c ConsumerConfig) ConsumerExchangeConfig {
	name := strings.ToLower(strings.TrimSpace(c.Exchange))
	if name == "" {
		name = "binance"
	}
	typ := name
	baseURL := defaultExchangeBaseURL(typ, c.MarketType, c.BinanceWSBaseURL)
	tickers := append([]string(nil), c.Tickers...)
	if len(tickers) == 0 {
		tickers = []string{"BTC-USDT", "ETH-USDT"}
	}
	marketType := strings.ToUpper(strings.TrimSpace(c.MarketType))
	if marketType == "" {
		marketType = "SPOT"
	}
	return ConsumerExchangeConfig{
		Name:       name,
		Type:       typ,
		BaseURL:    baseURL,
		Tickers:    tickers,
		MarketType: marketType,
	}
}

func defaultExchangeBaseURL(exchangeType, marketType, legacyBinanceBaseURL string) string {
	switch strings.ToLower(strings.TrimSpace(exchangeType)) {
	case "binance":
		mt := strings.ToUpper(strings.TrimSpace(marketType))
		legacy := strings.TrimSpace(legacyBinanceBaseURL)
		switch mt {
		case "USD_M_FUTURES", "COIN_M_FUTURES":
			// Protect multi-exchange configs from inheriting the legacy SPOT URL.
			// Only keep an explicit legacy override when it is already a futures URL.
			if strings.Contains(strings.ToLower(legacy), "fstream.binance.com") {
				return legacy
			}
			return "wss://fstream.binance.com/stream"
		default:
			if legacy != "" {
				return legacy
			}
			return "wss://stream.binance.com:9443/stream"
		}
	case "bybit":
		switch strings.ToUpper(strings.TrimSpace(marketType)) {
		case "USD_M_FUTURES":
			return "wss://stream.bybit.com/v5/public/linear"
		case "COIN_M_FUTURES":
			return "wss://stream.bybit.com/v5/public/inverse"
		default:
			return "wss://stream.bybit.com/v5/public/spot"
		}
	case "coinbase":
		return "wss://ws-feed.exchange.coinbase.com"
	case "hyperliquid":
		return "wss://api.hyperliquid.xyz/ws"
	case "kraken":
		return "wss://ws.kraken.com/v2"
	case "krakenf":
		return "wss://futures.kraken.com/ws/v1"
	default:
		return ""
	}
}

// stripComments removes // line comments and /* block comments */ from JSONC
// source while preserving newlines so that line numbers in JSON errors remain
// accurate.  It correctly ignores comment-like sequences inside string literals.
func stripComments(src []byte) []byte {
	type state int
	const (
		stNormal state = iota
		stString
		stEscape       // inside string after backslash
		stLineComment  // after //
		stBlockComment // after /*
		stBlockStar    // inside block comment after *
	)

	out := make([]byte, 0, len(src))
	st := stNormal
	i := 0
	for i < len(src) {
		b := src[i]
		switch st {
		case stNormal:
			if b == '"' {
				st = stString
				out = append(out, b)
			} else if b == '/' && i+1 < len(src) && src[i+1] == '/' {
				st = stLineComment
				i += 2
				continue
			} else if b == '/' && i+1 < len(src) && src[i+1] == '*' {
				st = stBlockComment
				i += 2
				continue
			} else {
				out = append(out, b)
			}
		case stString:
			out = append(out, b)
			switch b {
			case '\\':
				st = stEscape
			case '"':
				st = stNormal
			}
		case stEscape:
			out = append(out, b)
			st = stString
		case stLineComment:
			switch b {
			case '\n':
				out = append(out, b) // preserve newline for error line numbers
				st = stNormal
			}
			// else: consume comment character
		case stBlockComment:
			switch b {
			case '\n':
				out = append(out, b) // preserve newlines
			case '*':
				st = stBlockStar
			}
		case stBlockStar:
			if b == '/' {
				st = stNormal
			} else if b == '\n' {
				out = append(out, b)
				st = stBlockComment
			} else if b != '*' {
				st = stBlockComment
			}
		}
		i++
	}
	return out
}
