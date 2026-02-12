package config

import (
	"encoding/json"
	"fmt"
	"os"
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

// Load reads a JSONC config file and returns an AppConfig with defaults applied.
// If path is empty, Load returns a fully-defaulted AppConfig without reading any file.
// If the file exists but cannot be parsed, Load returns a non-nil *problem.Problem.
func Load(path string) (AppConfig, *problem.Problem) {
	var cfg AppConfig
	applyDefaults(&cfg)

	if path == "" {
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
	return nil
}

func validateBus(b BusConfig) *problem.Problem {
	switch strings.ToLower(strings.TrimSpace(b.Type)) {
	case "inmemory", "jetstream":
		return nil
	default:
		return problem.Newf(codeInvalid, "bus.type must be inmemory|jetstream, got %q", b.Type)
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
	for _, field := range []struct {
		name  string
		value string
	}{
		{"http.read_timeout", h.ReadTimeout},
		{"http.write_timeout", h.WriteTimeout},
		{"http.idle_timeout", h.IdleTimeout},
		{"http.shutdown_timeout", h.ShutdownTimeout},
	} {
		if _, err := time.ParseDuration(field.value); err != nil {
			return problem.Newf(codeInvalid, "%s: invalid duration %q: %v", field.name, field.value, err)
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
		if strings.EqualFold(ex.MarketType, "SPOT") && c.StreamsPerTicker != 2 {
			return problem.Newf(codeInvalid, "consumer.streams_per_ticker=%d incompatible with spot runtime baseline=2", c.StreamsPerTicker)
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
		if _, exists := seen[name]; exists {
			return problem.Newf(codeInvalid, "consumer.exchanges name must be unique, duplicate %q", ex.Name)
		}
		seen[name] = struct{}{}

		typ := strings.ToLower(strings.TrimSpace(ex.Type))
		switch typ {
		case "binance", "bybit":
		default:
			return problem.Newf(codeInvalid, "consumer.exchanges[%d].type must be binance|bybit, got %q", i, ex.Type)
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
	if c.Bus.Type == "" {
		c.Bus.Type = "inmemory"
	}
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
		c.JetStream.FilterSubjects = []string{"marketdata.bookdelta.>"}
	}
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
		c.MarketData.PublishContentType = envelope.ContentTypeJSON
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
	if c.Processor.BusCapacity == 0 {
		c.Processor.BusCapacity = 1024
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
		if strings.TrimSpace(legacyBinanceBaseURL) != "" {
			return strings.TrimSpace(legacyBinanceBaseURL)
		}
		return "wss://stream.binance.com:9443/stream"
	case "bybit":
		switch strings.ToUpper(strings.TrimSpace(marketType)) {
		case "USD_M_FUTURES":
			return "wss://stream.bybit.com/v5/public/linear"
		case "COIN_M_FUTURES":
			return "wss://stream.bybit.com/v5/public/inverse"
		default:
			return "wss://stream.bybit.com/v5/public/spot"
		}
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
