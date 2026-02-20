package main

import "testing"

func TestResolveTargetConfig(t *testing.T) {
	timescale, err := resolveTargetConfig("timescale")
	if err != nil {
		t.Fatalf("resolveTargetConfig(timescale): %v", err)
	}
	if timescale.dialect != "postgres" {
		t.Fatalf("timescale dialect=%q want=postgres", timescale.dialect)
	}
	if timescale.defaultDir != "sql/timescale/migrations" {
		t.Fatalf("timescale defaultDir=%q", timescale.defaultDir)
	}

	clickhouse, err := resolveTargetConfig("clickhouse")
	if err != nil {
		t.Fatalf("resolveTargetConfig(clickhouse): %v", err)
	}
	if clickhouse.driver != "clickhouse" {
		t.Fatalf("clickhouse driver=%q want=clickhouse", clickhouse.driver)
	}
	if clickhouse.defaultDir != "sql/clickhouse/migrations" {
		t.Fatalf("clickhouse defaultDir=%q", clickhouse.defaultDir)
	}
}

func TestResolveTargetConfig_Unknown(t *testing.T) {
	if _, err := resolveTargetConfig("unknown"); err == nil {
		t.Fatal("expected error for unknown target")
	}
}

func TestResolveDSN_Priority(t *testing.T) {
	t.Setenv("TEST_DSN", "env-dsn")

	if got := resolveDSN("flag-dsn", "TEST_DSN", "default-dsn"); got != "flag-dsn" {
		t.Fatalf("resolveDSN flag priority got=%q", got)
	}
	if got := resolveDSN("", "TEST_DSN", "default-dsn"); got != "env-dsn" {
		t.Fatalf("resolveDSN env priority got=%q", got)
	}
	if got := resolveDSN("", "TEST_DSN_MISSING", "default-dsn"); got != "default-dsn" {
		t.Fatalf("resolveDSN default fallback got=%q", got)
	}
}
