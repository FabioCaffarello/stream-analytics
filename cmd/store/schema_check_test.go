package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/market-raccoon/internal/shared/problem"
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
	// Write a temporary migration file missing the required pattern.
	tmpDir := t.TempDir()
	content := "CREATE TABLE IF NOT EXISTS aggregation_snapshots_v2 (venue String) ENGINE = MergeTree ORDER BY (venue);"
	if err := os.WriteFile(filepath.Join(tmpDir, "0002_w2_cold_correctness.sql"), []byte(content), 0o600); err != nil {
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
