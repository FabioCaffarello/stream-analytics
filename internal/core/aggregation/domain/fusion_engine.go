package domain

import (
	"slices"
	"strings"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/validation"
)

const fusionFixedPointScale = 100_000_000

// FusionDepthInput is one venue's orderbook snapshot for fusion.
type FusionDepthInput struct {
	Venue      string
	TsIngestMs int64
	Seq        int64
	Bids       []Level
	Asks       []Level
}

// FuseDepth merges orderbook snapshots from multiple venues into one fused view.
func FuseDepth(
	instrument string,
	nowMs int64,
	inputs []FusionDepthInput,
	mode FusionMode,
	staleThresholdMs int64,
) (FusedDepthSnapshotV1, *problem.Problem) {
	if p := validateFuseDepthArgs(instrument, nowMs, mode, staleThresholdMs); p != nil {
		return FusedDepthSnapshotV1{}, p
	}

	active, venueTimestamps := filterActiveDepthInputs(inputs, nowMs, staleThresholdMs)
	sourceMix := BuildSourceMix(venueTimestamps, nowMs, staleThresholdMs)
	staleness := BuildStalenessReport(sourceMix, nowMs)
	confidence := DeriveFusionConfidence(sourceMix)

	sourceVenues := make([]string, 0, len(active))
	for _, a := range active {
		sourceVenues = append(sourceVenues, a.Venue)
	}

	bids := fuseLevels(active, true, mode)
	asks := fuseLevels(active, false, mode)
	depthCapped := capFusedLevels(&bids, &asks)
	spreadBPS := fusionComputeSpread(bids, asks)
	tags := DeriveFeatureTags(confidence, sourceMix, 0, depthCapped)

	return FusedDepthSnapshotV1{
		Instrument:      strings.TrimSpace(instrument),
		TsServerMs:      nowMs,
		Mode:            mode,
		Bids:            bids,
		Asks:            asks,
		SourceVenues:    sourceVenues,
		GlobalSpreadBPS: spreadBPS,
		Meta: FusionMeta{
			Reason:      fusionReason(mode),
			Confidence:  confidence,
			SourceMix:   sourceMix,
			Staleness:   staleness,
			FeatureTags: tags,
		},
	}, nil
}

func validateFuseDepthArgs(instrument string, nowMs int64, mode FusionMode, staleThresholdMs int64) *problem.Problem {
	if p := validation.Collect(
		validation.NonEmptyString("instrument", instrument),
		validation.PositiveInt("now_ms", nowMs),
	); p != nil {
		return p
	}
	if !ValidFusionMode(mode) {
		return problem.New(problem.ValidationFailed, "fusion mode must be a recognized value")
	}
	if staleThresholdMs <= 0 {
		return problem.New(problem.ValidationFailed, "stale_threshold_ms must be > 0")
	}
	return nil
}

func filterActiveDepthInputs(inputs []FusionDepthInput, nowMs, staleThresholdMs int64) ([]FusionDepthInput, map[string]int64) {
	venueTimestamps := make(map[string]int64, len(inputs))
	active := make([]FusionDepthInput, 0, len(inputs))
	for _, inp := range inputs {
		v := strings.TrimSpace(inp.Venue)
		if v == "" {
			continue
		}
		venueTimestamps[v] = inp.TsIngestMs
		if inp.TsIngestMs <= 0 || nowMs-inp.TsIngestMs > staleThresholdMs {
			continue
		}
		if len(inp.Bids) == 0 && len(inp.Asks) == 0 {
			continue
		}
		active = append(active, inp)
	}
	slices.SortFunc(active, func(a, b FusionDepthInput) int {
		return strings.Compare(a.Venue, b.Venue)
	})
	return active, venueTimestamps
}

func capFusedLevels(bids, asks *[]FusedLevel) bool {
	capped := len(*bids) > FusedDepthMaxLevels || len(*asks) > FusedDepthMaxLevels
	if len(*bids) > FusedDepthMaxLevels {
		*bids = (*bids)[:FusedDepthMaxLevels]
	}
	if len(*asks) > FusedDepthMaxLevels {
		*asks = (*asks)[:FusedDepthMaxLevels]
	}
	return capped
}

func fusionComputeSpread(bids, asks []FusedLevel) float64 {
	if len(bids) > 0 && len(asks) > 0 {
		bestBid := float64(bids[0].PriceFP) / fusionFixedPointScale
		bestAsk := float64(asks[0].PriceFP) / fusionFixedPointScale
		return fusionSpreadBPS(bestBid, bestAsk)
	}
	return 0
}

func fuseLevels(inputs []FusionDepthInput, isBid bool, mode FusionMode) []FusedLevel {
	type priceKey int64
	type merged struct {
		priceFP int64
		sizeFP  int64
		venues  []string
	}
	index := make(map[priceKey]*merged)
	var keys []priceKey

	for _, inp := range inputs {
		levels := inp.Asks
		if isBid {
			levels = inp.Bids
		}
		for _, lvl := range levels {
			if lvl.Price <= 0 || lvl.Quantity <= 0 {
				continue
			}
			fp := fusionPriceToFP(lvl.Price)
			pk := priceKey(fp)
			if m, ok := index[pk]; ok {
				m.sizeFP += fusionQtyToFP(lvl.Quantity)
				if !slices.Contains(m.venues, inp.Venue) {
					m.venues = append(m.venues, inp.Venue)
				}
			} else {
				index[pk] = &merged{
					priceFP: fp,
					sizeFP:  fusionQtyToFP(lvl.Quantity),
					venues:  []string{inp.Venue},
				}
				keys = append(keys, pk)
			}
		}
	}

	result := make([]FusedLevel, 0, len(keys))
	for _, k := range keys {
		m := index[k]
		slices.Sort(m.venues)
		result = append(result, FusedLevel{
			PriceFP: m.priceFP,
			SizeFP:  m.sizeFP,
			Venues:  m.venues,
		})
	}

	if isBid {
		slices.SortFunc(result, func(a, b FusedLevel) int {
			if a.PriceFP > b.PriceFP {
				return -1
			}
			if a.PriceFP < b.PriceFP {
				return 1
			}
			return 0
		})
	} else {
		slices.SortFunc(result, func(a, b FusedLevel) int {
			if a.PriceFP < b.PriceFP {
				return -1
			}
			if a.PriceFP > b.PriceFP {
				return 1
			}
			return 0
		})
	}
	return result
}

func fusionPriceToFP(price Price) int64 {
	return int64(float64(price)*fusionFixedPointScale + 0.5)
}

func fusionQtyToFP(qty Quantity) int64 {
	return int64(float64(qty)*fusionFixedPointScale + 0.5)
}

func fusionSpreadBPS(bid, ask float64) float64 {
	if bid <= 0 || ask <= 0 || ask <= bid {
		return 0
	}
	mid := (bid + ask) / 2
	if mid <= 0 {
		return 0
	}
	return ((ask - bid) / mid) * 10_000
}

func fusionReason(mode FusionMode) string {
	switch mode {
	case FusionMerge:
		return "cross_venue_merge"
	case FusionWeighted:
		return "weighted_fusion"
	default:
		return "single_venue"
	}
}
