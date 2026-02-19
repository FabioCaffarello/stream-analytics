package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver for database/sql
	"github.com/pressly/goose/v3"
)

const defaultDSN = "postgres://raccoon:raccoon@localhost:5432/raccoon?sslmode=disable" //nolint:gosec // dev-only default

func main() {
	dsn := flag.String("dsn", "", "PostgreSQL DSN (default: $DATABASE_URL or local dev)")
	dir := flag.String("dir", "sql/timescale/migrations", "directory containing SQL migration files")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: migrate [flags] <command>")
		fmt.Fprintln(os.Stderr, "commands: up, down, status, redo, version, create <name>")
		os.Exit(1)
	}

	connStr := resolveDSN(*dsn)
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "migrate: open database: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	if err := db.PingContext(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: ping database: %v\n", err)
		os.Exit(1)
	}

	if err := goose.SetDialect("postgres"); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: set dialect: %v\n", err)
		os.Exit(1)
	}
	goose.SetTableName("goose_db_version")

	command := strings.ToLower(strings.TrimSpace(args[0]))
	cmdArgs := args[1:]

	if err := runCommand(db, *dir, command, cmdArgs); err != nil {
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

func resolveDSN(flagDSN string) string {
	if flagDSN != "" {
		return flagDSN
	}
	if env := os.Getenv("DATABASE_URL"); env != "" {
		return env
	}
	return defaultDSN
}
