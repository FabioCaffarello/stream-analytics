package marketmodel

import "github.com/market-raccoon/internal/shared/problem"

type Book struct {
	bids []Level
	asks []Level
}

func NewBook() Book {
	return Book{}
}

func (b *Book) ApplySnapshot(snapshot BookSnapshot) *problem.Problem {
	nb, na, p := NormalizeBookOrdering(snapshot.Bids, snapshot.Asks, false)
	if p != nil {
		return p
	}
	b.bids = nb
	b.asks = na
	return nil
}

func (b *Book) ApplyDelta(delta BookDelta) *problem.Problem {
	nb, na, p := NormalizeBookOrdering(delta.Bids, delta.Asks, true)
	if p != nil {
		return p
	}
	for _, bid := range nb {
		b.bids = applyLevel(b.bids, bid, true)
	}
	for _, ask := range na {
		b.asks = applyLevel(b.asks, ask, false)
	}
	return nil
}

func (b Book) Snapshot(ts int64) BookSnapshot {
	return BookSnapshot{
		Bids:      cloneLevels(b.bids),
		Asks:      cloneLevels(b.asks),
		Timestamp: ts,
	}
}

func (b Book) Bids() []Level {
	return cloneLevels(b.bids)
}

func (b Book) Asks() []Level {
	return cloneLevels(b.asks)
}

func applyLevel(levels []Level, update Level, desc bool) []Level {
	if len(levels) == 0 {
		if update.Size == 0 {
			return nil
		}
		return []Level{update}
	}
	idx, found := findLevel(levels, update.Price, desc)
	if update.Size == 0 {
		if !found {
			return levels
		}
		copy(levels[idx:], levels[idx+1:])
		return levels[:len(levels)-1]
	}
	if found {
		levels[idx] = update
		return levels
	}
	levels = append(levels, Level{})
	copy(levels[idx+1:], levels[idx:])
	levels[idx] = update
	return levels
}

func findLevel(levels []Level, price float64, desc bool) (idx int, found bool) {
	lo := 0
	hi := len(levels)
	target := price
	for lo < hi {
		mid := (lo + hi) / 2
		cur := levels[mid].Price
		if cur == target {
			return mid, true
		}
		if desc {
			if cur < target {
				hi = mid
			} else {
				lo = mid + 1
			}
			continue
		}
		if cur > target {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo, false
}
