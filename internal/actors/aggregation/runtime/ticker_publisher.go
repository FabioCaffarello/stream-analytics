package aggruntime

import (
	"context"
	"log/slog"
	"time"

	"github.com/anthdm/hollywood/actor"
)

type runtimeTicker interface {
	C() <-chan time.Time
	Stop()
}

type systemTicker struct {
	t *time.Ticker
}

func (s *systemTicker) C() <-chan time.Time { return s.t.C }
func (s *systemTicker) Stop()               { s.t.Stop() }

// TickerPublisherConfig configures timer-driven snapshot tick publishing.
type TickerPublisherConfig struct {
	Logger *slog.Logger
	Target *actor.PID

	OrderbookInterval time.Duration
	HeatmapInterval   time.Duration
	VolumeInterval    time.Duration

	NewTicker func(interval time.Duration) runtimeTicker
}

// TickerPublisherActor emits SnapshotTick messages for enabled intervals.
type TickerPublisherActor struct {
	cfg    TickerPublisherConfig
	logger *slog.Logger
	engine *actor.Engine

	stopCancel context.CancelFunc
}

// NewTickerPublisherActor returns a hollywood actor producer.
func NewTickerPublisherActor(cfg TickerPublisherConfig) actor.Producer {
	return func() actor.Receiver {
		return &TickerPublisherActor{cfg: cfg}
	}
}

func (t *TickerPublisherActor) Receive(c *actor.Context) {
	t.ensureDefaults()

	switch c.Message().(type) {
	case actor.Initialized:
		// no-op
	case actor.Started:
		t.onStarted(c)
	case actor.Stopped:
		t.onStopped()
	}
}

func (t *TickerPublisherActor) ensureDefaults() {
	if t.logger == nil {
		if t.cfg.Logger != nil {
			t.logger = t.cfg.Logger
		} else {
			t.logger = slog.Default()
		}
	}
	if t.cfg.NewTicker == nil {
		t.cfg.NewTicker = func(interval time.Duration) runtimeTicker {
			return &systemTicker{t: time.NewTicker(interval)}
		}
	}
}

func (t *TickerPublisherActor) onStarted(c *actor.Context) {
	target := t.cfg.Target
	if target == nil {
		target = c.Parent()
	}
	if target == nil {
		t.logger.Warn("aggruntime: ticker publisher has no target")
		return
	}

	t.engine = c.Engine()
	ctx, cancel := context.WithCancel(context.Background())
	t.stopCancel = cancel

	t.startTickerLoop(ctx, target, SnapshotTickOrderBook, t.cfg.OrderbookInterval)
	t.startTickerLoop(ctx, target, SnapshotTickHeatmap, t.cfg.HeatmapInterval)
	t.startTickerLoop(ctx, target, SnapshotTickVolume, t.cfg.VolumeInterval)
}

func (t *TickerPublisherActor) onStopped() {
	if t.stopCancel != nil {
		t.stopCancel()
	}
}

func (t *TickerPublisherActor) startTickerLoop(
	ctx context.Context,
	target *actor.PID,
	kind SnapshotTickKind,
	interval time.Duration,
) {
	if interval <= 0 {
		return
	}

	ticker := t.cfg.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C():
				if ctx.Err() != nil {
					return
				}
				t.engine.Send(target, SnapshotTick{Kind: kind})
			}
		}
	}()
}
