package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"

	_ "github.com/ClickHouse/clickhouse-go/v2" // clickhouse driver for database/sql
	_ "github.com/jackc/pgx/v5/stdlib"         // pgx driver for database/sql
	"github.com/pressly/goose/v3"
)

const defaultTimescaleDSN = "postgres://raccoon:raccoon@localhost:5432/raccoon?sslmode=disable" //nolint:gosec // dev-only default
const defaultClickHouseDSN = "clickhouse://default:password@localhost:9000/default"             //nolint:gosec // dev-only default

type targetConfig struct {
	driver     string
	dialect    string
	defaultDir string
	dsnEnv     string
	defaultDSN string
}

func main() {
	target := flag.String("target", "timescale", "migration target: timescale|clickhouse")
	dsn := flag.String("dsn", "", "database DSN (default: target-specific env var or local dev)")
	dir := flag.String("dir", "", "directory containing SQL migration files (default: target-specific)")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: migrate [flags] <command>")
		fmt.Fprintln(os.Stderr, "targets: timescale|clickhouse")
		fmt.Fprintln(os.Stderr, "commands: up, down, status, redo, version, create <name>")
		os.Exit(1)
	}

	cfg, err := resolveTargetConfig(*target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
	migrationDir := strings.TrimSpace(*dir)
	if migrationDir == "" {
		migrationDir = cfg.defaultDir
	}

	connStr := resolveDSN(*dsn, cfg.dsnEnv, cfg.defaultDSN)
	db, err := sql.Open(cfg.driver, connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "migrate: open database: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	if err := db.PingContext(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: ping database: %v\n", err)
		os.Exit(1)
	}

	if err := goose.SetDialect(cfg.dialect); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: set dialect: %v\n", err)
		os.Exit(1)
	}
	goose.SetTableName("goose_db_version")

	command := strings.ToLower(strings.TrimSpace(args[0]))
	cmdArgs := args[1:]

	if err := runCommand(db, migrationDir, command, cmdArgs); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
}

func runCommand(db *sql.DB, dir, command string, args []string) error {
	ctx := context.Background()
	switch command {
	case "up":
		return goose.UpContext(ctx, db, dir)
	case "down":
		return goose.DownContext(ctx, db, dir)
	case "status":
		return goose.StatusContext(ctx, db, dir)
	case "redo":
		return goose.RedoContext(ctx, db, dir)
	case "version":
		return goose.VersionContext(ctx, db, dir)
	case "create":
		if len(args) == 0 {
			return fmt.Errorf("create requires a migration name")
		}
		return goose.Create(db, dir, args[0], "sql")
	default:
		return fmt.Errorf("unknown command %q (allowed: up, down, status, redo, version, create)", command)
	}
}

func resolveTargetConfig(target string) (targetConfig, error) {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "timescale", "postgres", "postgresql":
		return targetConfig{
			driver:     "pgx",
			dialect:    "postgres",
			defaultDir: "sql/timescale/migrations",
			dsnEnv:     "DATABASE_URL",
			defaultDSN: defaultTimescaleDSN,
		}, nil
	case "clickhouse":
		return targetConfig{
			driver:     "clickhouse",
			dialect:    "clickhouse",
			defaultDir: "sql/clickhouse/migrations",
			dsnEnv:     "CLICKHOUSE_DSN",
			defaultDSN: defaultClickHouseDSN,
		}, nil
	default:
		return targetConfig{}, fmt.Errorf("unknown target %q (allowed: timescale, clickhouse)", target)
	}
}

func resolveDSN(flagDSN, envVar, defaultDSN string) string {
	if flagDSN != "" {
		return flagDSN
	}
	if env := os.Getenv(envVar); env != "" {
		return env
	}
	return defaultDSN
}
