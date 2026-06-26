package aggruntime

import (
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/metrics"
)

func (p *ProcessorSubsystemActor) handleSnapshotTick(msg SnapshotTick) {
	if p.shuttingDown || p.cfg.PublishEnvelope == nil {
		return
	}
	now := p.clockNow()
	p.emitOrderBookStaleDurations(now)
	if p.shouldDeferSnapshotTick(now, msg.Kind) {
		return
	}

	switch msg.Kind {
	case SnapshotTickOrderBook:
		p.publishOrderBookSnapshots()
	case SnapshotTickHeatmap:
		p.publishHeatmapSnapshots()
	case SnapshotTickVolume:
		p.publishVolumeSnapshots()
	}
}

func (p *ProcessorSubsystemActor) shouldDeferSnapshotTick(now time.Time, kind SnapshotTickKind) bool {
	if p.hbLastTsIngest <= 0 {
		return false
	}
	nowMs := now.UnixMilli()
	if nowMs <= p.hbLastTsIngest {
		return false
	}
	skew := time.Duration(nowMs-p.hbLastTsIngest) * time.Millisecond
	if skew <= snapshotTickDeferSkewThreshold {
		return false
	}
	if shouldEmitHeartbeat(now, p.snapshotTickDeferLastLogAt, false, snapshotTickDeferLogInterval) {
		p.snapshotTickDeferLastLogAt = now
		p.logger.Info("aggruntime: deferring periodic snapshot tick while processor catches up",
			"kind", msgKindString(kind),
			"ingest_skew", skew.String(),
			"last_ts_ingest", p.hbLastTsIngest,
		)
	}
	return true
}

func (p *ProcessorSubsystemActor) shouldSkipBookDeltaForCatchUp(env envelope.Envelope) bool {
	if p.cfg.CatchUpSkipBookDeltaSkew <= 0 || env.TsIngest <= 0 || p.hbLastTsIngest <= 0 {
		return false
	}
	if p.hbLastTsIngest <= env.TsIngest {
		return false
	}
	skew := time.Duration(p.hbLastTsIngest-env.TsIngest) * time.Millisecond
	if skew <= p.cfg.CatchUpSkipBookDeltaSkew {
		return false
	}
	now := p.clockNow()
	if shouldEmitHeartbeat(now, p.bookDeltaCatchUpSkipLastLogAt, false, snapshotTickDeferLogInterval) {
		p.bookDeltaCatchUpSkipLastLogAt = now
		p.logger.Info("aggruntime: skipping stale bookdelta while processor catches up",
			"ingest_skew", skew.String(),
			"threshold", p.cfg.CatchUpSkipBookDeltaSkew.String(),
			"watermark_ts_ingest", p.hbLastTsIngest,
			"envelope_ts_ingest", env.TsIngest,
			"venue", env.Venue,
			"instrument", env.Instrument,
			"seq", env.Seq,
		)
	}
	metrics.IncIngestDrop("bookdelta_catchup_skip")
	return true
}

func (p *ProcessorSubsystemActor) shouldSkipTradeForCatchUp(env envelope.Envelope) bool {
	if p.cfg.CatchUpSkipTradeSkew <= 0 || env.TsIngest <= 0 || p.hbLastTsIngest <= 0 {
		return false
	}
	if p.hbLastTsIngest <= env.TsIngest {
		return false
	}
	skew := time.Duration(p.hbLastTsIngest-env.TsIngest) * time.Millisecond
	if skew <= p.cfg.CatchUpSkipTradeSkew {
		return false
	}
	now := p.clockNow()
	if shouldEmitHeartbeat(now, p.tradeCatchUpSkipLastLogAt, false, snapshotTickDeferLogInterval) {
		p.tradeCatchUpSkipLastLogAt = now
		p.logger.Info("aggruntime: skipping stale trade while processor catches up",
			"ingest_skew", skew.String(),
			"threshold", p.cfg.CatchUpSkipTradeSkew.String(),
			"watermark_ts_ingest", p.hbLastTsIngest,
			"envelope_ts_ingest", env.TsIngest,
			"venue", env.Venue,
			"instrument", env.Instrument,
			"seq", env.Seq,
		)
	}
	metrics.IncIngestDrop("trade_catchup_skip")
	return true
}

func (p *ProcessorSubsystemActor) shouldSkipLiquidationForCatchUp(env envelope.Envelope) bool {
	if p.cfg.CatchUpSkipStatsSkew <= 0 || env.TsIngest <= 0 || p.hbLastTsIngest <= 0 {
		return false
	}
	if p.hbLastTsIngest <= env.TsIngest {
		return false
	}
	skew := time.Duration(p.hbLastTsIngest-env.TsIngest) * time.Millisecond
	if skew <= p.cfg.CatchUpSkipStatsSkew {
		return false
	}
	now := p.clockNow()
	if shouldEmitHeartbeat(now, p.liquidationCatchUpSkipLogAt, false, snapshotTickDeferLogInterval) {
		p.liquidationCatchUpSkipLogAt = now
		p.logger.Info("aggruntime: skipping stale liquidation while processor catches up",
			"ingest_skew", skew.String(),
			"threshold", p.cfg.CatchUpSkipStatsSkew.String(),
			"watermark_ts_ingest", p.hbLastTsIngest,
			"envelope_ts_ingest", env.TsIngest,
			"venue", env.Venue,
			"instrument", env.Instrument,
			"seq", env.Seq,
		)
	}
	metrics.IncIngestDrop("liquidation_catchup_skip")
	return true
}

func (p *ProcessorSubsystemActor) shouldSkipMarkPriceForCatchUp(env envelope.Envelope) bool {
	if p.cfg.CatchUpSkipStatsSkew <= 0 || env.TsIngest <= 0 || p.hbLastTsIngest <= 0 {
		return false
	}
	if p.hbLastTsIngest <= env.TsIngest {
		return false
	}
	skew := time.Duration(p.hbLastTsIngest-env.TsIngest) * time.Millisecond
	if skew <= p.cfg.CatchUpSkipStatsSkew {
		return false
	}
	now := p.clockNow()
	if shouldEmitHeartbeat(now, p.markPriceCatchUpSkipLogAt, false, snapshotTickDeferLogInterval) {
		p.markPriceCatchUpSkipLogAt = now
		p.logger.Info("aggruntime: skipping stale markprice while processor catches up",
			"ingest_skew", skew.String(),
			"threshold", p.cfg.CatchUpSkipStatsSkew.String(),
			"watermark_ts_ingest", p.hbLastTsIngest,
			"envelope_ts_ingest", env.TsIngest,
			"venue", env.Venue,
			"instrument", env.Instrument,
			"seq", env.Seq,
		)
	}
	metrics.IncIngestDrop("markprice_catchup_skip")
	return true
}

func msgKindString(kind SnapshotTickKind) string {
	if kind == "" {
		return "unknown"
	}
	return string(kind)
}
