package domain

import "math"

// GroupedLevel represents a deterministic grouped price bucket.
type GroupedLevel struct {
	Price              Price
	TotalQuantity      Quantity
	LevelCount         int
	CumulativeQuantity Quantity
}

// GroupLevels groups sorted raw levels into deterministic floor buckets.
// Input levels must be pre-sorted by caller according to side semantics.
// maxRows <= 0 means uncapped output.
func GroupLevels(levels []Level, groupSize float64, maxRows int) []GroupedLevel {
	if len(levels) == 0 {
		return nil
	}
	if !isFiniteFloat(groupSize) || groupSize <= 0 {
		groupSize = 1
	}
	limit := maxRows
	if limit <= 0 {
		limit = len(levels)
	}
	out := make([]GroupedLevel, 0, min(len(levels), limit))

	for i := range levels {
		price := float64(levels[i].Price)
		qty := float64(levels[i].Quantity)
		if !isFiniteFloat(price) || !isFiniteFloat(qty) || price <= 0 || qty < 0 {
			continue
		}
		bucket := math.Floor(price/groupSize) * groupSize
		if len(out) > 0 && float64(out[len(out)-1].Price) == bucket {
			out[len(out)-1].TotalQuantity += levels[i].Quantity
			out[len(out)-1].LevelCount++
			continue
		}
		if maxRows > 0 && len(out) >= maxRows {
			break
		}
		out = append(out, GroupedLevel{
			Price:         Price(bucket),
			TotalQuantity: levels[i].Quantity,
			LevelCount:    1,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// FillCumulative fills cumulative quantity from best to worst.
func FillCumulative(grouped []GroupedLevel) {
	if len(grouped) == 0 {
		return
	}
	running := Quantity(0)
	for i := range grouped {
		running += grouped[i].TotalQuantity
		grouped[i].CumulativeQuantity = running
	}
}

func isFiniteFloat(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
