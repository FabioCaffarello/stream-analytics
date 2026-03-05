package util

import "core:testing"
import "mr:ports"

@(test)
test_build_subject_normalizes_venue_and_symbol :: proc(t: ^testing.T) {
	subject := build_subject_with_timeframe("binance-spot", "BTCUSDT:SPOT", .Trades, "")
	defer delete(subject)
	testing.expect_value(t, subject, "marketdata.trade/binance/BTCUSDT/raw")
}

@(test)
test_build_subject_normalizes_symbol_separators :: proc(t: ^testing.T) {
	subject := build_subject_with_timeframe("coinbase", "BTC-USD", .Candles, "1m")
	defer delete(subject)
	testing.expect_value(t, subject, "aggregation.candle/coinbase/BTCUSD/1m")
}

@(test)
test_build_subject_orderbook_uses_aggregation_snapshot_stream :: proc(t: ^testing.T) {
	subject := build_subject_with_timeframe("binance", "BTCUSDT", .Orderbook, "")
	defer delete(subject)
	testing.expect_value(t, subject, "aggregation.snapshot/binance/BTCUSDT/raw")
}

@(test)
test_build_signal_subject_uses_canonical_signal_stream :: proc(t: ^testing.T) {
	subject := build_subject_with_timeframe("binance-perp", "BTCUSDT", .Signals, "5m")
	defer delete(subject)
	testing.expect_value(t, subject, "signal/binance/BTCUSDT/5m")
}

@(test)
test_build_evidence_subject_uses_liquidity_evidence_stream :: proc(t: ^testing.T) {
	subject := build_subject_with_timeframe("binance", "BTCUSDT", .Evidence, "")
	defer delete(subject)
	testing.expect_value(t, subject, "liquidity.evidence/binance/BTCUSDT/raw")
}

@(test)
test_subject_id64_for_stream_uses_canonical_market_key :: proc(t: ^testing.T) {
	a := subject_id64_for_stream("binance-spot", "BTCUSDT:SPOT", ports.MD_Channel.Trades)
	b := subject_id64_for_stream("binance", "BTCUSDT", ports.MD_Channel.Trades)
	testing.expect_value(t, a, b)
}

@(test)
test_build_subject_rejects_invalid_control_bytes :: proc(t: ^testing.T) {
	raw_venue_bytes := [7]u8{0, 0, 0, 0, 0, 0, 0}
	raw_symbol_bytes := [12]u8{0, 0, 0, 0, 0, 0, 0, 0, 16, 0, 0, 0}
	subject := build_subject_with_timeframe(string(raw_venue_bytes[:]), string(raw_symbol_bytes[:]), .Stats, "")
	defer delete(subject)
	testing.expect_value(t, subject, "")
}

@(test)
test_subject_id64_for_stream_rejects_invalid_market_key :: proc(t: ^testing.T) {
	raw_venue_bytes := [7]u8{0, 0, 0, 0, 0, 0, 0}
	raw_symbol_bytes := [12]u8{0, 0, 0, 0, 0, 0, 0, 0, 16, 0, 0, 0}
	id := subject_id64_for_stream(string(raw_venue_bytes[:]), string(raw_symbol_bytes[:]), ports.MD_Channel.Stats)
	testing.expect_value(t, id, u64(0))
}
