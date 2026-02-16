package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
)

// schemaContracts defines the DDL patterns that must be present in the
// ClickHouse migration files for the store pipeline to function correctly.
var schemaContracts = []struct {
	file     string
	patterns []string
}{
	{
		file: "0002_w2_cold_correctness.sql",
		patterns: []string{
			"CREATE TABLE IF NOT EXISTS aggregation_snapshots_v2",
			"source_idempotency_key",
			"ORDER BY (subject, venue, instrument, seq, source_idempotency_key)",
		},
	},
}

// ValidateSchemaContract reads SQL migration files from migrationsDir and
// verifies that they contain the expected DDL patterns.  Returns a Problem
// if any required pattern is missing, allowing the store to fail fast on
// startup when the schema is incompatible.
func ValidateSchemaContract(migrationsDir string) *problem.Problem {
	for _, contract := range schemaContracts {
		path := filepath.Join(migrationsDir, contract.file)
		raw, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return problem.Newf(problem.ValidationFailed,
				"schema contract: cannot read migration %s: %v", contract.file, err)
		}
		ddl := string(raw)
		for _, pattern := range contract.patterns {
			if !strings.Contains(ddl, pattern) {
				return problem.Newf(problem.ValidationFailed,
					"schema contract: migration %s missing required pattern %q", contract.file, pattern)
			}
		}
	}
	return nil
}
