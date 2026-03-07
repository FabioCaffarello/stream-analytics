package aggruntime

import (
	"context"
	"strconv"
	"strings"
	"time"

	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/naming"
	"github.com/market-raccoon/internal/shared/problem"
)

const (
	typeFusedDepth    = "aggregation.fused_depth"
	fusedDepthVenue   = "GLOBAL"
	fusedDepthVersion = 1
)

// ProcessorFusionConfig controls multi-venue fusion behavior.
type ProcessorFusionConfig struct {
	Enabled        bool
	StaleThreshold time.Duration
	Mode           aggdomain.FusionMode
}

// handleBookDeltaForFusion produces fused depth snapshots when fusion is enabled.
func (p *ProcessorSubsystemActor) handleBookDeltaForFusion(env envelope.Envelope, instrumentKey string) *problem.Problem {
	if !p.cfg.Fusion.Enabled || p.cfg.PublishEnvelope == nil {
		return nil
	}
	if p.cfg.Service == nil || p.cfg.Service.UpdateBook == nil {
		return nil
	}

	nowMs := env.TsIngest
	if nowMs <= 0 {
		return problem.New(problem.ValidationFailed, "fusion merge requires ts_ingest > 0")
	}

	venueBooks := p.crossVenueBooks[instrumentKey]
	if len(venueBooks) < 2 {
		return nil
	}

	inputs := make([]aggdomain.FusionDepthInput, 0, len(venueBooks))
	for _, vb := range venueBooks {
		snapshot, prob := p.cfg.Service.UpdateBook.Snapshot(vb.Venue, instrumentKey)
		if prob != nil {
			continue
		}
		inputs = append(inputs, aggdomain.FusionDepthInput{
			Venue:      vb.Venue,
			TsIngestMs: vb.TsIngest,
			Seq:        snapshot.Seq,
			Bids:       snapshot.Bids,
			Asks:       snapshot.Asks,
		})
	}

	if len(inputs) < 2 {
		return nil
	}

	mode := p.cfg.Fusion.Mode
	if mode == "" {
		mode = aggdomain.FusionMerge
	}
	staleMs := p.cfg.Fusion.StaleThreshold.Milliseconds()
	if staleMs <= 0 {
		staleMs = defaultXVenueStaleThreshold.Milliseconds()
	}

	mergeStart := time.Now()
	fused, mergeProb := aggdomain.FuseDepth(
		naming.StripMarketType(instrumentKey),
		nowMs,
		inputs,
		mode,
		staleMs,
	)
	metrics.ObserveMRXVenueMergeDuration(instrumentKey, time.Since(mergeStart))
	if mergeProb != nil {
		return mergeProb
	}

	if len(fused.Bids) == 0 && len(fused.Asks) == 0 {
		return nil
	}

	seq := p.nextFusionSeq(instrumentKey)
	outEnv, prob := buildFusedDepthEnvelope(env, instrumentKey, seq, fused)
	if prob != nil {
		return prob
	}
	return p.cfg.PublishEnvelope.Publish(context.Background(), outEnv)
}

func (p *ProcessorSubsystemActor) nextFusionSeq(instrumentKey string) int64 {
	if p.fusionSeq == nil {
		p.fusionSeq = make(map[string]int64)
	}
	next := p.fusionSeq[instrumentKey] + 1
	if next <= 0 {
		next = 1
	}
	p.fusionSeq[instrumentKey] = next
	return next
}

func buildFusedDepthEnvelope(
	trigger envelope.Envelope,
	instrumentKey string,
	seq int64,
	fused aggdomain.FusedDepthSnapshotV1,
) (envelope.Envelope, *problem.Problem) {
	if seq <= 0 {
		return envelope.Envelope{}, problem.New(problem.ValidationFailed, "fused depth seq must be > 0")
	}
	ct := resolveContentType(typeFusedDepth)
	payload, p := codec.EncodePayload(typeFusedDepth, fusedDepthVersion, ct, fused)
	if p != nil {
		return envelope.Envelope{}, p
	}

	meta := map[string]string{
		"fusion_mode":       string(fused.Mode),
		"fusion_confidence": fusionConfidenceStr(fused.Meta.Confidence),
		"source_venues":     strings.Join(fused.SourceVenues, ","),
	}
	if marketType := envelopeMarketType(trigger); marketType != "" {
		meta[metaKeyMarketType] = marketType
	}

	out := envelope.Envelope{
		Type:        typeFusedDepth,
		Version:     fusedDepthVersion,
		Venue:       fusedDepthVenue,
		Instrument:  naming.StripMarketType(instrumentKey),
		TsExchange:  trigger.TsExchange,
		TsIngest:    trigger.TsIngest,
		Seq:         seq,
		ContentType: ct,
		Meta:        meta,
		Payload:     payload,
		IdempotencyKey: sharedhash.HashFieldsFast(
			typeFusedDepth,
			strconv.Itoa(fusedDepthVersion),
			strings.ToUpper(strings.TrimSpace(instrumentKey)),
			strconv.FormatInt(seq, 10),
			strings.TrimSpace(trigger.IdempotencyKey),
		),
	}
	if p := out.Validate(); p != nil {
		return envelope.Envelope{}, p
	}
	return out, nil
}

func fusionConfidenceStr(c float64) string {
	switch {
	case c >= 0.9:
		return "high"
	case c >= 0.5:
		return "medium"
	default:
		return "low"
	}
}
