package main

import (
	"log/slog"
	"strings"
	"testing"
)

func TestNewE2ERuntime_RequiresExplicitTestPosture(t *testing.T) {
	t.Setenv(envE2ETestMode, "1")
	t.Setenv(envProcessorRunMode, "prod")
	t.Setenv(envProcessorMode, "")

	rt, p := newE2ERuntime(slog.Default())
	if p == nil {
		t.Fatal("expected e2e posture validation error")
	}
	if !strings.Contains(p.Message, "requires RUN_MODE=test or STREAM_ANALYTICS_MODE=test") {
		t.Fatalf("unexpected message: %q", p.Message)
	}
	if rt != nil {
		t.Fatal("runtime should be nil when posture is invalid")
	}
}

func TestNewE2ERuntime_AllowsTestPosture(t *testing.T) {
	t.Setenv(envE2ETestMode, "1")
	t.Setenv(envProcessorRunMode, "")
	t.Setenv(envProcessorMode, "test")

	rt, p := newE2ERuntime(slog.Default())
	if p != nil {
		t.Fatalf("unexpected posture validation error: %v", p)
	}
	if rt == nil || !rt.enabled {
		t.Fatal("expected enabled runtime")
	}
}

func TestResolveProcessorLoopbackProbeAddr_AlwaysLoopback(t *testing.T) {
	t.Setenv(envE2EHTTPAddr, "0.0.0.0:19082")
	if got := resolveProcessorLoopbackProbeAddr(); got != "127.0.0.1:19082" {
		t.Fatalf("resolveProcessorLoopbackProbeAddr=%q want=%q", got, "127.0.0.1:19082")
	}
}
