package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

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
	if prob := validateConsumer(a.Consumer); prob != nil {
		return prob
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
	if strings.TrimSpace(c.Exchange) == "" {
		return problem.New(codeInvalid, "consumer.exchange must not be empty")
	}
	if len(c.Tickers) == 0 {
		return problem.New(codeInvalid, "consumer.tickers must not be empty")
	}
	if c.FakeRateMs <= 0 {
		return problem.Newf(codeInvalid, "consumer.fake_rate_ms must be > 0, got %d", c.FakeRateMs)
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
	for _, field := range []struct {
		name  string
		value string
	}{
		{"consumer.max_websocket_lifetime", c.MaxWebsocketLifetime},
		{"consumer.respawn_overlap", c.RespawnOverlap},
	} {
		if _, err := time.ParseDuration(field.value); err != nil {
			return problem.Newf(codeInvalid, "%s: invalid duration %q: %v", field.name, field.value, err)
		}
	}
	if c.BinanceReal {
		if !strings.EqualFold(c.Exchange, "binance") {
			return problem.New(codeInvalid, "consumer.binance_real requires consumer.exchange=binance")
		}
		if strings.TrimSpace(c.BinanceWSBaseURL) == "" {
			return problem.New(codeInvalid, "consumer.binance_ws_base_url must not be empty when consumer.binance_real=true")
		}
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
	if c.Consumer.Exchange == "" {
		c.Consumer.Exchange = "binance"
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
	if c.Consumer.FakeRateMs == 0 {
		c.Consumer.FakeRateMs = 500
	}
	if !c.Consumer.BinanceReal && !c.Consumer.Fake {
		// W3 explicit mode: fake is default unless binance_real is enabled.
		c.Consumer.Fake = true
	}
	if c.Processor.BusCapacity == 0 {
		c.Processor.BusCapacity = 1024
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
			if b == '\\' {
				st = stEscape
			} else if b == '"' {
				st = stNormal
			}
		case stEscape:
			out = append(out, b)
			st = stString
		case stLineComment:
			if b == '\n' {
				out = append(out, b) // preserve newline for error line numbers
				st = stNormal
			}
			// else: consume comment character
		case stBlockComment:
			if b == '\n' {
				out = append(out, b) // preserve newlines
			} else if b == '*' {
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
