package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

func TestValidateSchemaContract_Valid(t *testing.T) {
	dir := filepath.Join("..", "..", "sql", "clickhouse", "migrations")
	if p := ValidateSchemaContract(dir); p != nil {
		t.Fatalf("expected valid, got: %v", p)
	}
}

func TestValidateSchemaContract_MissingFile(t *testing.T) {
	p := ValidateSchemaContract("/nonexistent/migrations")
	if p == nil {
		t.Fatal("expected problem for missing file, got nil")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("code=%q want=%q", p.Code, problem.ValidationFailed)
	}
}

func TestValidateSchemaContract_IncompatibleDDL(t *testing.T) {
	// Write temporary migration files; v2 has wrong DDL, v3 is valid.
	tmpDir := t.TempDir()
	v2Content := "CREATE TABLE IF NOT EXISTS aggregation_snapshots_v2 (venue String) ENGINE = MergeTree ORDER BY (venue);"
	if err := os.WriteFile(filepath.Join(tmpDir, "0002_w2_cold_correctness.sql"), []byte(v2Content), 0o600); err != nil {
		t.Fatal(err)
	}
	v3Content := "CREATE TABLE IF NOT EXISTS aggregation_snapshots_v3 (ts DateTime64(3)) ENGINE = ReplacingMergeTree PARTITION BY toYYYYMM(ts) ORDER BY (subject) TTL toDateTime(ts) + INTERVAL 90 DAY;"
	if err := os.WriteFile(filepath.Join(tmpDir, "0003_w4_ttl_partition.sql"), []byte(v3Content), 0o600); err != nil {
		t.Fatal(err)
	}

	p := ValidateSchemaContract(tmpDir)
	if p == nil {
		t.Fatal("expected problem for incompatible DDL, got nil")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("code=%q want=%q", p.Code, problem.ValidationFailed)
	}
}

func TestValidateSchemaContract_V3MissingTTL(t *testing.T) {
	tmpDir := t.TempDir()
	v2Content := "CREATE TABLE IF NOT EXISTS aggregation_snapshots_v2 (source_idempotency_key String) ENGINE = ReplacingMergeTree ORDER BY (subject, venue, instrument, seq, source_idempotency_key);"
	if err := os.WriteFile(filepath.Join(tmpDir, "0002_w2_cold_correctness.sql"), []byte(v2Content), 0o600); err != nil {
		t.Fatal(err)
	}
	// v3 without TTL clause.
	v3Content := "CREATE TABLE IF NOT EXISTS aggregation_snapshots_v3 (ts DateTime64(3)) ENGINE = ReplacingMergeTree PARTITION BY toYYYYMM(ts) ORDER BY (subject, venue, instrument, seq, source_idempotency_key);"
	if err := os.WriteFile(filepath.Join(tmpDir, "0003_w4_ttl_partition.sql"), []byte(v3Content), 0o600); err != nil {
		t.Fatal(err)
	}

	p := ValidateSchemaContract(tmpDir)
	if p == nil {
		t.Fatal("expected problem for missing TTL clause, got nil")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("code=%q want=%q", p.Code, problem.ValidationFailed)
	}
}
