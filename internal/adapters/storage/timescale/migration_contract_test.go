package timescale_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTimescaleSchemaContractM2HasSubMinuteRetentionPolicy(t *testing.T) {
	path := filepath.Clean(filepath.Join("..", "..", "..", "..", "sql", "timescale", "migrations", "0004_m2_subminute_retention_policy.sql"))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read schema %s: %v", path, err)
	}
	ddl := string(raw)
	if !strings.Contains(ddl, "CREATE OR REPLACE FUNCTION cleanup_aggregation_hot_retention") {
		t.Fatalf("migration must define cleanup_aggregation_hot_retention function")
	}
	if !strings.Contains(ddl, "aggregation_candle") || !strings.Contains(ddl, "aggregation_stats") {
		t.Fatalf("migration must cover both aggregation_candle and aggregation_stats")
	}
	if !strings.Contains(ddl, "timeframe IN ('1s', '5s')") {
		t.Fatalf("migration must define sub-minute timeframe retention branch")
	}
	if !strings.Contains(ddl, "INTERVAL '14 days'") {
		t.Fatalf("migration must define 14-day retention for sub-minute timeframes")
	}
	if !strings.Contains(ddl, "INTERVAL '90 days'") {
		t.Fatalf("migration must define 90-day retention for non-sub-minute timeframes")
	}
}
