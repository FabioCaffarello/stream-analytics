package deliveryruntime

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	deliveryv1 "github.com/market-raccoon/internal/shared/proto/gen/delivery/v1"
)

type logCaptureHandler struct {
	mu      sync.Mutex
	warnMsg []string
}

func (h *logCaptureHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn
}

func (h *logCaptureHandler) Handle(_ context.Context, rec slog.Record) error {
	if rec.Level < slog.LevelWarn {
		return nil
	}
	h.mu.Lock()
	h.warnMsg = append(h.warnMsg, rec.Message)
	h.mu.Unlock()
	return nil
}

func (h *logCaptureHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *logCaptureHandler) WithGroup(_ string) slog.Handler      { return h }

func (h *logCaptureHandler) warnCount(msg string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	count := 0
	for _, item := range h.warnMsg {
		if item == msg {
			count++
		}
	}
	return count
}

func TestBackpressureStrategy_ActionHintDeterministic(t *testing.T) {
	strategy := defaultBackpressureStrategy()
	tests := []struct {
		reason string
		want   deliveryv1.ActionHint
	}{
		{reason: backpressureDropReasonQueueFull, want: deliveryv1.ActionHint_ACTION_HINT_RECONNECT},
		{reason: backpressureDropReasonPriorityDropSelf, want: deliveryv1.ActionHint_ACTION_HINT_RECONNECT},
		{reason: backpressureDropReasonSlowClientDisconnect, want: deliveryv1.ActionHint_ACTION_HINT_RECONNECT},
		{reason: backpressureDropReasonFrameTooLarge, want: deliveryv1.ActionHint_ACTION_HINT_NONE},
		{reason: "unknown_reason", want: deliveryv1.ActionHint_ACTION_HINT_RECONNECT},
	}
	for _, tt := range tests {
		if got := strategy.actionHintForDrop(tt.reason); got != tt.want {
			t.Fatalf("reason=%q action_hint=%q want=%q", tt.reason, got.String(), tt.want.String())
		}
	}
}

func TestBackpressureDropSampling_NoSpam(t *testing.T) {
	h := &logCaptureHandler{}
	sa := &SessionActor{
		logger: slog.New(h),
		bpStrategy: backpressureStrategy{
			elevatedRatio:    defaultBackpressureElevatedRatio,
			highRatio:        defaultBackpressureHighRatio,
			criticalRatio:    defaultBackpressureCriticalRatio,
			sampleTopN:       2,
			sampleMaxUnique:  8,
			sampleFlushEvery: 16,
		},
		bpDropSamples: make(map[backpressureDropSampleKey]int),
	}

	for i := 0; i < 200; i++ {
		reason := backpressureDropReasonQueueFull
		if i%9 == 0 {
			reason = backpressureDropReasonFrameTooLarge
		}
		channel := "trade"
		if i%7 == 0 {
			channel = "stats"
		}
		sa.recordBackpressureDropSample(reason, channel, "standard")
	}
	sa.flushBackpressureDropSamples(true)

	const sampledMsg = "delivery session: sampled backpressure drops"
	if got := h.warnCount(sampledMsg); got != 26 {
		t.Fatalf("sampled warn count=%d want=26", got)
	}
}
