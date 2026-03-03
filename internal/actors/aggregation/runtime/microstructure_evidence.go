package aggruntime

import (
	"encoding/json"
	"math"
	"sort"
	"strings"

	aggapp "github.com/market-raccoon/internal/core/aggregation/app"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/envelope"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/problem"
)

const microstructureEvidenceType = "insights.microstructure_evidence"

type microstructureEvidenceKind string

const (
	evidenceLiqImbalance    microstructureEvidenceKind = "LIQ_IMBALANCE"
	evidenceAbsorption      microstructureEvidenceKind = "ABSORPTION"
	evidenceSpreadExplosion microstructureEvidenceKind = "SPREAD_EXPLOSION"
	evidenceLiquidityThin   microstructureEvidenceKind = "LIQUIDITY_THINNING"
)

type microstructureEvidenceV1 struct {
	Kind          string    `json:"kind"`
	Confidence    float64   `json:"confidence"`
	Features      []string  `json:"features"`
	FeatureValues []float64 `json:"feature_values"`
	Reason        string    `json:"reason"`
	TsIngest      int64     `json:"ts_ingest"`
	Seq           int64     `json:"seq"`
}

func detectMicrostructureEvidence(req aggapp.UpdateRequest, resp aggapp.UpdateResponse) []microstructureEvidenceV1 {
	features := computeMicrostructureFeatures(req, resp)
	out := make([]microstructureEvidenceV1, 0, 4)

	if features.absImbalance >= 0.65 {
		side := "buy"
		if features.imbalance < 0 {
			side = "sell"
		}
		out = append(out, microstructureEvidenceV1{
			Kind:          string(evidenceLiqImbalance),
			Confidence:    clamp01(0.45 + (features.absImbalance-0.65)*1.2),
			Features:      []string{"imbalance", "top_liquidity"},
			FeatureValues: []float64{features.imbalance, features.topLiquidity},
			Reason:        "top-of-book imbalance skewed to " + side,
			TsIngest:      0,
			Seq:           req.Seq,
		})
	}
	if features.spreadBps >= 18 {
		out = append(out, microstructureEvidenceV1{
			Kind:          string(evidenceSpreadExplosion),
			Confidence:    clamp01(0.4 + features.spreadBps/120.0),
			Features:      []string{"spread_bps", "mid_price"},
			FeatureValues: []float64{features.spreadBps, features.mid},
			Reason:        "spread expanded beyond microstructure baseline",
			TsIngest:      0,
			Seq:           req.Seq,
		})
	}
	if features.topLiquidity <= 18 {
		out = append(out, microstructureEvidenceV1{
			Kind:          string(evidenceLiquidityThin),
			Confidence:    clamp01(1.0 - features.topLiquidity/25.0),
			Features:      []string{"top_liquidity", "top_depth_levels"},
			FeatureValues: []float64{features.topLiquidity, float64(features.levelsUsed)},
			Reason:        "visible top-book liquidity is thin",
			TsIngest:      0,
			Seq:           req.Seq,
		})
	}
	if !req.IsSnapshot && features.deltaFlow >= 20 && features.spreadBps <= 6 {
		out = append(out, microstructureEvidenceV1{
			Kind:          string(evidenceAbsorption),
			Confidence:    clamp01(0.35 + features.deltaFlow/120.0),
			Features:      []string{"delta_flow", "spread_bps"},
			FeatureValues: []float64{features.deltaFlow, features.spreadBps},
			Reason:        "high orderflow while spread stayed compressed",
			TsIngest:      0,
			Seq:           req.Seq,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Confidence > out[j].Confidence
	})
	return out
}

type microstructureFeatures struct {
	imbalance    float64
	absImbalance float64
	topLiquidity float64
	deltaFlow    float64
	mid          float64
	spreadBps    float64
	levelsUsed   int
}

func computeMicrostructureFeatures(req aggapp.UpdateRequest, resp aggapp.UpdateResponse) microstructureFeatures {
	const maxLevels = 5
	bidTop, bidQty := weightedTop(req.Bids, maxLevels)
	askTop, askQty := weightedTop(req.Asks, maxLevels)
	topLiquidity := bidQty + askQty
	imb := 0.0
	if topLiquidity > 0 {
		imb = (bidQty - askQty) / topLiquidity
	}

	mid := 0.0
	if bidTop > 0 && askTop > 0 {
		mid = (bidTop + askTop) * 0.5
	}
	spreadBps := 0.0
	if mid > 0 && resp.Spread > 0 {
		spreadBps = (resp.Spread / mid) * 10_000.0
	}
	return microstructureFeatures{
		imbalance:    imb,
		absImbalance: math.Abs(imb),
		topLiquidity: topLiquidity,
		deltaFlow:    sumQty(req.Bids) + sumQty(req.Asks),
		mid:          mid,
		spreadBps:    spreadBps,
		levelsUsed:   minInt(maxLevels, minInt(len(req.Bids), len(req.Asks))),
	}
}

func weightedTop(levels []aggdomain.Level, n int) (topPrice, weightedQty float64) {
	if len(levels) == 0 || n <= 0 {
		return 0, 0
	}
	limit := minInt(len(levels), n)
	topPrice = float64(levels[0].Price)
	for i := 0; i < limit; i++ {
		weight := float64(limit - i)
		weightedQty += float64(levels[i].Quantity) * weight
	}
	return topPrice, weightedQty
}

func sumQty(levels []aggdomain.Level) float64 {
	total := 0.0
	for i := range levels {
		total += float64(levels[i].Quantity)
	}
	return total
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func buildMicrostructureEvidenceEnvelope(src envelope.Envelope, evt microstructureEvidenceV1) (envelope.Envelope, *problem.Problem) {
	payload, err := json.Marshal(evt)
	if err != nil {
		return envelope.Envelope{}, problem.Wrap(err, problem.Internal, "marshal microstructure evidence payload")
	}
	e := envelope.Envelope{
		Type:           microstructureEvidenceType,
		Version:        1,
		Venue:          src.Venue,
		Instrument:     src.Instrument,
		TsExchange:     src.TsExchange,
		TsIngest:       src.TsIngest,
		Seq:            src.Seq,
		IdempotencyKey: sharedhash.HashFieldsFast(src.IdempotencyKey, strings.ToLower(evt.Kind)),
		ContentType:    envelope.ContentTypeJSON,
		Meta: map[string]string{
			metaKeyMarketType: envelopeMarketType(src),
			metaKeyTimeframe:  defaultInsightsTimeframe,
		},
		Payload: payload,
	}
	if p := e.Validate(); p != nil {
		return envelope.Envelope{}, p
	}
	return e, nil
}
