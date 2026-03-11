package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"marketmonkey/pkg/db"
	"marketmonkey/pkg/db/clickhouse"
	"marketmonkey/pkg/db/timescale"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/jackc/pgx/v5"
)

func WaitTillShutdown(e *actor.Engine, pids ...*actor.PID) {
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	<-interrupt

	var wg sync.WaitGroup

	for _, pid := range pids {
		wg.Add(1)
		go func(pid *actor.PID) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
			defer cancel()
			defer wg.Done()
			<-e.PoisonCtx(ctx, pid).Done()
		}(pid)
	}
	wg.Wait()
	slog.Info("shutdown complete")
}

func CreateDBClient() (db.Client, error) {
	engine := os.Getenv("DATABASE_ENGINE")
	if len(engine) == 0 {
		engine = "timescaledb"
	}

	if engine == "timescaledb" {
		addr := os.Getenv("TIMESCALE_ADDR")
		dbName := os.Getenv("TIMESCALE_DB")
		username := os.Getenv("TIMESCALE_USER")
		password := os.Getenv("TIMESCALE_PASSWORD")
		if len(addr) == 0 || len(dbName) == 0 || len(username) == 0 || len(password) == 0 {
			return nil, fmt.Errorf("you FORGOT to set the TIMESCALE_ADDR, TIMESCALE_DB, TIMESCALE_USER, TIMESCALE_PASSWORD in the environment")
		}
		conn, err := pgx.Connect(context.Background(), fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable", username, password, addr, dbName))
		if err != nil {
			return nil, err
		}
		return timescale.NewClient(conn)
	}

	if engine == "clickhouse" {
		addr := os.Getenv("CLICKHOUSE_ADDR")
		dbName := os.Getenv("CLICKHOUSE_DB")
		username := os.Getenv("CLICKHOUSE_USER")
		password := os.Getenv("CLICKHOUSE_PASSWORD")
		if len(addr) == 0 || len(dbName) == 0 || len(username) == 0 || len(password) == 0 {
			return nil, fmt.Errorf("you FORGOT to set the CLICKHOUSE_ADDR, CLICKHOUSE_DB, CLICKHOUSE_USER, CLICKHOUSE_PASSWORD in the environment")
		}
		return clickhouse.NewClient(addr, dbName, username, password)
	}
	return nil, fmt.Errorf("invalid database engine: %s", engine)
}
