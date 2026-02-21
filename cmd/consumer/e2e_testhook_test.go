//go:build integration

package main

import (
	"log/slog"
	"strings"
	"testing"
)

func TestNewE2ERuntime_RequiresExplicitTestPosture(t *testing.T) {
	t.Setenv(envConsumerE2ETestMode, "1")
	t.Setenv(envRunMode, "prod")
	t.Setenv(envMarketRaccoonMode, "")

	rt, p := newE2ERuntime(slog.Default())
	if p == nil {
		t.Fatal("expected e2e posture validation error")
	}
	if !strings.Contains(p.Message, "requires RUN_MODE=test or MARKET_RACCOON_MODE=test") {
		t.Fatalf("unexpected message: %q", p.Message)
	}
	if rt != nil {
		t.Fatal("runtime should be nil when posture is invalid")
	}
}

func TestNewE2ERuntime_AllowsTestPosture(t *testing.T) {
	t.Setenv(envConsumerE2ETestMode, "1")
	t.Setenv(envRunMode, "test")
	t.Setenv(envMarketRaccoonMode, "")

	rt, p := newE2ERuntime(slog.Default())
	if p != nil {
		t.Fatalf("unexpected posture validation error: %v", p)
	}
	if rt == nil || !rt.enabled {
		t.Fatal("expected enabled runtime")
	}
}

func TestResolveLoopbackProbeAddr_AlwaysLoopback(t *testing.T) {
	t.Setenv(envConsumerE2EHTTPAddr, "0.0.0.0:19083")
	t.Setenv(envConsumerProbeAddr, "")
	if got := resolveLoopbackProbeAddr(); got != "127.0.0.1:19083" {
		t.Fatalf("resolveLoopbackProbeAddr=%q want=%q", got, "127.0.0.1:19083")
	}

	t.Setenv(envConsumerE2EHTTPAddr, "[::]:19084")
	if got := resolveLoopbackProbeAddr(); got != "127.0.0.1:19084" {
		t.Fatalf("resolveLoopbackProbeAddr=%q want=%q", got, "127.0.0.1:19084")
	}
}
