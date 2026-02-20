package storage_test

import (
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
)

// testSnapshot provides a small deterministic snapshot used by multiple tests.
func testSnapshot() aggdomain.SnapshotProduced {
	return aggdomain.SnapshotProduced{
		BookID: aggdomain.BookID{Venue: "binance", Instrument: "BTCUSDT"},
		Seq:    42,
		Bids: []aggdomain.Level{{
			Price:    100,
			Quantity: 1,
		}},
		Asks: []aggdomain.Level{{
			Price:    101,
			Quantity: 1,
		}},
	}
}
