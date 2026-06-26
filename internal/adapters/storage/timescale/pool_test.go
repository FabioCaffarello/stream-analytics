package timescale_test

import (
	"context"
	"testing"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/adapters/storage/timescale"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

func TestNewPool_EmptyDSN(t *testing.T) {
	_, p := timescale.NewPool(context.Background(), timescale.PoolConfig{})
	if p == nil {
		t.Fatal("expected problem, got nil")
	}
	if p.Code != problem.InvalidArgument {
		t.Fatalf("code=%q want=%q", p.Code, problem.InvalidArgument)
	}
}

func TestNewPool_InvalidDSN(t *testing.T) {
	_, p := timescale.NewPool(context.Background(), timescale.PoolConfig{
		DSN: "://bad",
	})
	if p == nil {
		t.Fatal("expected problem, got nil")
	}
	if p.Code != problem.InvalidArgument {
		t.Fatalf("code=%q want=%q", p.Code, problem.InvalidArgument)
	}
}

func TestNewPool_UnreachableHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, p := timescale.NewPool(ctx, timescale.PoolConfig{
		DSN: "postgres://user:pass@127.0.0.1:65534/test?sslmode=disable",
	})
	if p == nil {
		t.Fatal("expected problem for unreachable host, got nil")
	}
	if p.Code != problem.Unavailable {
		t.Fatalf("code=%q want=%q", p.Code, problem.Unavailable)
	}
}
