package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── Load ──────────────────────────────────────────────────────────────────────

func TestLoad_EmptyPath_ReturnsDefaults(t *testing.T) {
	cfg, prob := Load("")
	if prob != nil {
		t.Fatalf("Load(\"\") unexpectedly failed: %v", prob)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("default log.level = %q, want %q", cfg.Log.Level, "info")
	}
	if cfg.HTTP.Addr != ":8080" {
		t.Errorf("default http.addr = %q, want %q", cfg.HTTP.Addr, ":8080")
	}
	if cfg.Consumer.Exchange != "binance" {
		t.Errorf("default consumer.exchange = %q, want %q", cfg.Consumer.Exchange, "binance")
	}
	if len(cfg.Consumer.Tickers) == 0 {
		t.Error("default consumer.tickers should not be empty")
	}
	if cfg.Consumer.FakeRateMs != 500 {
		t.Errorf("default consumer.fake_rate_ms = %d, want 500", cfg.Consumer.FakeRateMs)
	}
	if !cfg.Consumer.Fake {
		t.Errorf("default consumer.fake = %v, want true", cfg.Consumer.Fake)
	}
	if cfg.Consumer.StreamsPerTicker != 2 {
		t.Errorf("default consumer.streams_per_ticker = %d, want 2", cfg.Consumer.StreamsPerTicker)
	}
	if cfg.Consumer.MaxStreamsPerWebsocket != 200 {
		t.Errorf("default consumer.max_streams_per_websocket = %d, want 200", cfg.Consumer.MaxStreamsPerWebsocket)
	}
	if cfg.Consumer.MaxWebsockets != 5 {
		t.Errorf("default consumer.max_websockets = %d, want 5", cfg.Consumer.MaxWebsockets)
	}
	if cfg.Consumer.BinanceWSBaseURL == "" {
		t.Error("default consumer.binance_ws_base_url should not be empty")
	}
	if cfg.Processor.BusCapacity != 1024 {
		t.Errorf("default processor.bus_capacity = %d, want 1024", cfg.Processor.BusCapacity)
	}
}

func TestLoad_NonExistentFile_ReturnsNotFound(t *testing.T) {
	_, prob := Load("/tmp/does-not-exist-market-raccoon.jsonc")
	if prob == nil {
		t.Fatal("expected problem for non-existent file, got nil")
	}
	if prob.Code != codeNotFound {
		t.Errorf("problem code = %q, want %q", prob.Code, codeNotFound)
	}
}

func TestLoad_ValidJSONC_ParsesFields(t *testing.T) {
	src := `{
		// log settings
		"log": { "level": "debug", "format": "json" },
		"http": {
			"addr": ":9090",   /* custom port */
			"read_timeout": "5s",
			"write_timeout": "10s",
			"idle_timeout": "30s",
			"shutdown_timeout": "8s"
		},
		"consumer": {
			"exchange": "kraken",
			"tickers": ["BTC-USD"],
			"fake": true,
			"binance_real": false,
			"binance_ws_base_url": "wss://stream.binance.com:9443/stream",
			"streams_per_ticker": 2,
			"max_streams_per_websocket": 200,
			"max_websockets": 3,
			"max_websocket_lifetime": "10m",
			"respawn_overlap": "2s",
			"fake_rate_ms": 250
		},
		"processor": { "bus_capacity": 512 }
	}`
	path := writeTempFile(t, src)

	cfg, prob := Load(path)
	if prob != nil {
		t.Fatalf("Load failed: %v", prob)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("log.level = %q, want debug", cfg.Log.Level)
	}
	if cfg.Log.Format != "json" {
		t.Errorf("log.format = %q, want json", cfg.Log.Format)
	}
	if cfg.HTTP.Addr != ":9090" {
		t.Errorf("http.addr = %q, want :9090", cfg.HTTP.Addr)
	}
	if cfg.HTTP.ShutdownTimeoutDuration() != 8*time.Second {
		t.Errorf("shutdown_timeout = %v, want 8s", cfg.HTTP.ShutdownTimeoutDuration())
	}
	if cfg.Consumer.Exchange != "kraken" {
		t.Errorf("consumer.exchange = %q, want kraken", cfg.Consumer.Exchange)
	}
	if len(cfg.Consumer.Tickers) != 1 || cfg.Consumer.Tickers[0] != "BTC-USD" {
		t.Errorf("consumer.tickers = %v, want [BTC-USD]", cfg.Consumer.Tickers)
	}
	if cfg.Consumer.FakeRateMs != 250 {
		t.Errorf("consumer.fake_rate_ms = %d, want 250", cfg.Consumer.FakeRateMs)
	}
	if cfg.Consumer.MaxWebsockets != 3 {
		t.Errorf("consumer.max_websockets = %d, want 3", cfg.Consumer.MaxWebsockets)
	}
	if cfg.Consumer.RespawnOverlapDuration() != 2*time.Second {
		t.Errorf("consumer.respawn_overlap = %v, want 2s", cfg.Consumer.RespawnOverlapDuration())
	}
	if cfg.Processor.BusCapacity != 512 {
		t.Errorf("processor.bus_capacity = %d, want 512", cfg.Processor.BusCapacity)
	}
}

func TestLoad_InvalidJSON_ReturnsParseError(t *testing.T) {
	path := writeTempFile(t, `{ "log": { "level": "debug" `)
	_, prob := Load(path)
	if prob == nil {
		t.Fatal("expected parse error for invalid JSON, got nil")
	}
	if prob.Code != codeParseError {
		t.Errorf("problem code = %q, want %q", prob.Code, codeParseError)
	}
}

func TestLoad_PartialFile_FillsRemainingDefaults(t *testing.T) {
	path := writeTempFile(t, `{"log": {"level": "warn"}}`)
	cfg, prob := Load(path)
	if prob != nil {
		t.Fatalf("Load failed: %v", prob)
	}
	if cfg.Log.Level != "warn" {
		t.Errorf("log.level = %q, want warn", cfg.Log.Level)
	}
	// Unspecified field should be defaulted.
	if cfg.HTTP.Addr != ":8080" {
		t.Errorf("http.addr = %q, want :8080 (default)", cfg.HTTP.Addr)
	}
}

// ── Validate ─────────────────────────────────────────────────────────────────

func TestValidate_Defaults_Passes(t *testing.T) {
	cfg, _ := Load("")
	if prob := cfg.Validate(); prob != nil {
		t.Fatalf("default config should pass validation, got: %v", prob)
	}
}

func TestValidate_InvalidLogLevel(t *testing.T) {
	cfg, _ := Load("")
	cfg.Log.Level = "VERBOSE"
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for invalid log level")
	}
	if prob.Code != codeInvalid {
		t.Errorf("problem code = %q, want %q", prob.Code, codeInvalid)
	}
}

func TestValidate_InvalidLogFormat(t *testing.T) {
	cfg, _ := Load("")
	cfg.Log.Format = "yaml"
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for invalid log format")
	}
}

func TestValidate_EmptyHTTPAddr(t *testing.T) {
	cfg, _ := Load("")
	cfg.HTTP.Addr = "   "
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for empty http.addr")
	}
}

func TestValidate_InvalidDuration(t *testing.T) {
	cfg, _ := Load("")
	cfg.HTTP.ReadTimeout = "not-a-duration"
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for invalid duration")
	}
}

func TestValidate_BinanceRealRequiresBinanceExchange(t *testing.T) {
	cfg, _ := Load("")
	cfg.Consumer.BinanceReal = true
	cfg.Consumer.Exchange = "kraken"
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for invalid exchange with binance_real")
	}
}

func TestValidate_BinanceRealInvalidRespawnOverlap(t *testing.T) {
	cfg, _ := Load("")
	cfg.Consumer.BinanceReal = true
	cfg.Consumer.RespawnOverlap = "nope"
	prob := cfg.Validate()
	if prob == nil {
		t.Fatal("expected validation error for invalid respawn overlap")
	}
}

// ── stripComments ─────────────────────────────────────────────────────────────

func TestStripComments_LineComment(t *testing.T) {
	in := `{"a": 1} // comment`
	out := string(stripComments([]byte(in)))
	if strings.Contains(out, "comment") {
		t.Errorf("line comment not stripped; got: %s", out)
	}
	if !strings.Contains(out, `"a": 1`) {
		t.Errorf("JSON content removed unexpectedly; got: %s", out)
	}
}

func TestStripComments_BlockComment(t *testing.T) {
	in := `{"a": /* the value */ 1}`
	out := string(stripComments([]byte(in)))
	if strings.Contains(out, "the value") {
		t.Errorf("block comment not stripped; got: %s", out)
	}
	if !strings.Contains(out, `"a":`) {
		t.Errorf("JSON content removed unexpectedly; got: %s", out)
	}
}

func TestStripComments_URLInsideString_NotStripped(t *testing.T) {
	in := `{"url": "https://example.com/api"}`
	out := string(stripComments([]byte(in)))
	if !strings.Contains(out, "https://example.com/api") {
		t.Errorf("URL inside string was incorrectly stripped; got: %s", out)
	}
}

func TestStripComments_PreservesNewlines(t *testing.T) {
	in := "{\n// comment\n\"a\": 1\n}"
	out := string(stripComments([]byte(in)))
	// Should have at least 3 newlines (before comment, replacing comment, after)
	count := strings.Count(out, "\n")
	if count < 3 {
		t.Errorf("expected newlines preserved, got %d newline(s) in: %q", count, out)
	}
}

func TestStripComments_EscapedQuoteInsideString(t *testing.T) {
	in := `{"msg": "say \"hello\" // not a comment"}`
	out := string(stripComments([]byte(in)))
	if !strings.Contains(out, `say \"hello\"`) {
		t.Errorf("escaped quote handling broken; got: %s", out)
	}
	if !strings.Contains(out, "not a comment") {
		t.Errorf("content after escaped quote in string was stripped; got: %s", out)
	}
}

func TestStripComments_BlockCommentPreservesNewlines(t *testing.T) {
	in := "{\n/*\nmulti\nline\n*/\n\"a\": 1\n}"
	out := string(stripComments([]byte(in)))
	// Block comment's internal newlines should be preserved
	count := strings.Count(out, "\n")
	if count < 4 {
		t.Errorf("expected block comment newlines preserved, got %d in %q", count, out)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "config-*.jsonc")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()
	return filepath.Clean(f.Name())
}
