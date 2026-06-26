package clickhouse_test

import (
	"context"
	"testing"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/adapters/storage/clickhouse"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

func TestNewPool_EmptyAddrs(t *testing.T) {
	_, p := clickhouse.NewPool(context.Background(), clickhouse.PoolConfig{
		Database: "default",
	})
	if p == nil {
		t.Fatal("expected problem, got nil")
	}
	if p.Code != problem.InvalidArgument {
		t.Fatalf("code=%q want=%q", p.Code, problem.InvalidArgument)
	}
}

func TestNewPool_EmptyDatabase(t *testing.T) {
	_, p := clickhouse.NewPool(context.Background(), clickhouse.PoolConfig{
		Addrs: []string{"127.0.0.1:9000"},
	})
	if p == nil {
		t.Fatal("expected problem, got nil")
	}
	if p.Code != problem.InvalidArgument {
		t.Fatalf("code=%q want=%q", p.Code, problem.InvalidArgument)
	}
}

func TestNewPool_UnreachableHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, p := clickhouse.NewPool(ctx, clickhouse.PoolConfig{
		Addrs:       []string{"127.0.0.1:65535"},
		Database:    "default",
		Username:    "default",
		DialTimeout: 10 * time.Millisecond,
		ReadTimeout: 10 * time.Millisecond,
	})
	if p == nil {
		t.Fatal("expected problem for unreachable host, got nil")
	}
	if p.Code != problem.Unavailable {
		t.Fatalf("code=%q want=%q", p.Code, problem.Unavailable)
	}
}
