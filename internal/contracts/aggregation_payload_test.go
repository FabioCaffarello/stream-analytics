package contracts

import (
	"encoding/json"
	"testing"
)

func TestSnapshotV2_RoundTrip_JSON(t *testing.T) {
	in := AggregationSnapshotV2{
		Venue:        "binance",
		Instrument:   "BTC-USDT",
		Seq:          42,
		Bids:         []AggregationOrderBookLevelV1{{Price: 65000, Quantity: 1.25}},
		Asks:         []AggregationOrderBookLevelV1{{Price: 65001, Quantity: 0.75}},
		BestBidPrice: 65000,
		BestAskPrice: 65001,
		SpreadBPS:    0.1538,
		Checksum:     12345,
		TsIngestMs:   1700000000000,
		BidCount:     10,
		AskCount:     11,
		DepthCap:     50,
		Version:      2,
	}

	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var out AggregationSnapshotV2
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if out.Version != 2 || out.Checksum != in.Checksum || out.DepthCap != in.DepthCap || out.Seq != in.Seq {
		t.Fatalf("roundtrip mismatch: got=%+v want=%+v", out, in)
	}
	if len(out.Bids) != 1 || len(out.Asks) != 1 {
		t.Fatalf("roundtrip levels mismatch bids=%d asks=%d", len(out.Bids), len(out.Asks))
	}
}

func TestSnapshotV2_BackwardCompat_V1Client(t *testing.T) {
	in := AggregationSnapshotV2{
		Venue:        "binance",
		Instrument:   "BTC-USDT",
		Seq:          42,
		Bids:         []AggregationOrderBookLevelV1{{Price: 65000, Quantity: 1.25}},
		Asks:         []AggregationOrderBookLevelV1{{Price: 65001, Quantity: 0.75}},
		BestBidPrice: 65000,
		BestAskPrice: 65001,
		SpreadBPS:    0.1538,
		Checksum:     12345,
		TsIngestMs:   1700000000000,
		BidCount:     10,
		AskCount:     11,
		DepthCap:     50,
		Version:      2,
	}

	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var out AggregationSnapshotV1
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal V1: %v", err)
	}
	if out.Venue != in.Venue || out.Instrument != in.Instrument || out.Seq != in.Seq {
		t.Fatalf("v1 compat mismatch: got=%+v want venue=%s instrument=%s seq=%d", out, in.Venue, in.Instrument, in.Seq)
	}
	if len(out.Bids) != 1 || len(out.Asks) != 1 {
		t.Fatalf("v1 compat levels mismatch bids=%d asks=%d", len(out.Bids), len(out.Asks))
	}
}
