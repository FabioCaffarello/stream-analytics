package deliveryruntime

import (
	"strings"

	"github.com/market-raccoon/internal/shared/metrics"
)

// EffectiveLimits captures resolved per-session limits after tenant/global
// default resolution. It replaces the 15-line block in ensureDefaults() and
// the inline struct building in emitHello().
type EffectiveLimits struct {
	MaxSubscriptions        int
	MaxSignalSubscriptions  int
	MaxSymbolsPerConnection int
	MaxFrameBytes           int
	OutboundQueueSize       int
	RateLimit               RateLimitConfig
	MetricsCadenceMs        int
	KeepaliveIntervalMs     int
	CompressThresholdBytes  int
	SlowClientDropThreshold int
}

// NewEffectiveLimits resolves all session defaults from config, centralizing
// the scattered defaulting that was previously in ensureDefaults().
func NewEffectiveLimits(cfg SessionConfig) EffectiveLimits {
	el := EffectiveLimits{
		MaxSubscriptions:        cfg.MaxSubscriptions,
		MaxSignalSubscriptions:  cfg.MaxSignalSubscriptions,
		MaxSymbolsPerConnection: cfg.MaxSymbolsPerConnection,
		RateLimit:               cfg.RateLimit,
		SlowClientDropThreshold: cfg.SlowClientDropThreshold,
	}
	el.MaxFrameBytes = cfg.MaxFrameBytes
	if el.MaxFrameBytes <= 0 {
		el.MaxFrameBytes = readLimitBytes
	}
	el.OutboundQueueSize = cfg.OutboundQueueSize
	if el.OutboundQueueSize <= 0 {
		el.OutboundQueueSize = 256
	}
	el.CompressThresholdBytes = wsCompressThresholdBytes

	keepalive := cfg.KeepaliveInterval
	if keepalive <= 0 {
		keepalive = wsKeepalivePingInterval
	}
	el.KeepaliveIntervalMs = int(keepalive.Milliseconds())

	cadence := cfg.MetricsCadence
	if cadence <= 0 {
		cadence = wsMetricsCadence
	}
	el.MetricsCadenceMs = int(cadence.Milliseconds())
	return el
}

// EmitMetrics publishes the 5 effective-limit gauges to Prometheus.
func (el EffectiveLimits) EmitMetrics() {
	metrics.SetWSEffectiveLimit(wsLimitTypeMaxSubscriptions, el.MaxSubscriptions)
	metrics.SetWSEffectiveLimit(wsLimitTypeMaxSymbols, el.MaxSymbolsPerConnection)
	metrics.SetWSEffectiveLimit(wsLimitTypeMaxFrameBytes, el.MaxFrameBytes)
	metrics.SetWSEffectiveLimit(wsLimitTypeOutboundQueue, el.OutboundQueueSize)
	rateLimit := 0
	if el.RateLimit.Enabled {
		rateLimit = el.RateLimit.MaxPerSecond
	}
	metrics.SetWSEffectiveLimit(wsLimitTypeRateLimit, rateLimit)
}

// ToHelloCapabilities builds the capabilities payload for the hello frame.
func (el EffectiveLimits) ToHelloCapabilities(serverInstanceID string, compressionEnabled bool) wsHelloCapabilities {
	var rl *wsHelloRateLimit
	if el.RateLimit.Enabled {
		rl = &wsHelloRateLimit{
			Enabled:       true,
			MaxPerSecond:  el.RateLimit.MaxPerSecond,
			BurstCapacity: el.RateLimit.BurstCapacity,
		}
	}
	features := []string{"batching", "snapshot_hash", "prev_seq"}
	if compressionEnabled {
		features = append(features, "compress")
	}
	return wsHelloCapabilities{
		Topics: []string{
			"marketdata.trade",
			"marketdata.bookdelta",
			"aggregation.snapshot",
			"aggregation.stats",
			"aggregation.candle",
			"aggregation.tape",
			"liquidity.evidence",
			"insights.heatmap_snapshot",
			"insights.volume_profile_snapshot",
			"signal",
		},
		Venues: []string{
			"binance",
			"bybit",
			"coinbase",
			"kraken",
			"hyperliquid",
		},
		MaxSubscriptionsPerConn:       el.MaxSubscriptions,
		MaxSignalSubscriptionsPerConn: el.MaxSignalSubscriptions,
		MaxSymbolsPerConnection:       el.MaxSymbolsPerConnection,
		MaxFrameBytes:                 el.MaxFrameBytes,
		OutboundQueueSize:             el.OutboundQueueSize,
		MetricsCadenceMs:              el.MetricsCadenceMs,
		KeepaliveIntervalMs:           el.KeepaliveIntervalMs,
		RateLimit:                     rl,
		SupportedFeatures:             features,
	}
}

// ── NegotiatedFeatures ──────────────────────────────────────────────────────
//
// Bitfield replacing the O(N) []string scan for client-negotiated features.

// NegotiatedFeatures is a compact bitfield for negotiated protocol features.
type NegotiatedFeatures struct{ bits uint8 }

const (
	featureBatching     uint8 = 1 << iota // 0x01
	featureCompress                       // 0x02
	featureSnapshotHash                   // 0x04
	featurePrevSeq                        // 0x08
)

var featureNameToBit = map[string]uint8{
	"batching":      featureBatching,
	"compress":      featureCompress,
	"snapshot_hash": featureSnapshotHash,
	"prev_seq":      featurePrevSeq,
}

// NegotiateFeatures validates requested features and returns a NegotiatedFeatures
// bitfield plus the list of valid feature names (for JSON serialization in ack).
// Returns unknown features separately for error reporting.
func NegotiateFeatures(requested []string, compressionEnabled bool) (NegotiatedFeatures, []string, []string) {
	var nf NegotiatedFeatures
	seen := make(map[string]struct{}, len(requested))
	var valid, unknown []string
	for _, raw := range requested {
		f := strings.ToLower(strings.TrimSpace(raw))
		if f == "" {
			continue
		}
		if _, dup := seen[f]; dup {
			continue
		}
		seen[f] = struct{}{}
		bit, ok := featureNameToBit[f]
		if !ok {
			unknown = append(unknown, f)
			continue
		}
		if f == "compress" && !compressionEnabled {
			unknown = append(unknown, f)
			continue
		}
		nf.bits |= bit
		valid = append(valid, f)
	}
	return nf, valid, unknown
}

// Has returns true if the named feature was negotiated. O(1).
func (nf NegotiatedFeatures) Has(name string) bool {
	bit, ok := featureNameToBit[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return false
	}
	return nf.bits&bit != 0
}

// HasBatching is a direct accessor for the batching feature.
func (nf NegotiatedFeatures) HasBatching() bool { return nf.bits&featureBatching != 0 }

// HasCompression is a direct accessor for the compress feature.
func (nf NegotiatedFeatures) HasCompression() bool { return nf.bits&featureCompress != 0 }

// List returns the negotiated feature names for JSON serialization.
func (nf NegotiatedFeatures) List() []string {
	var out []string
	if nf.bits&featureBatching != 0 {
		out = append(out, "batching")
	}
	if nf.bits&featureCompress != 0 {
		out = append(out, "compress")
	}
	if nf.bits&featureSnapshotHash != 0 {
		out = append(out, "snapshot_hash")
	}
	if nf.bits&featurePrevSeq != 0 {
		out = append(out, "prev_seq")
	}
	return out
}
