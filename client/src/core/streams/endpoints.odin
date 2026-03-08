package streams

import "core:strings"
import "mr:ports"
import "mr:util"

Endpoint_ID :: enum u8 {
	Unknown,
	Binance,
	Bybit,
	Coinbase,
	Kraken,
	Hyperliquid,
}

Endpoint_Capabilities :: struct {
	channel_mask: u16,  // S98: widened from u8 to accommodate analytics channels (13 total)
}

Stream_Endpoint :: struct {
	id:           Endpoint_ID,
	venue:        string,
	capabilities: Endpoint_Capabilities,
}

@(private = "file")
ENDPOINT_ALL_CHANNELS :: u16(
	(1 << u16(ports.MD_Channel.Trades)) |
	(1 << u16(ports.MD_Channel.Orderbook)) |
	(1 << u16(ports.MD_Channel.Stats)) |
	(1 << u16(ports.MD_Channel.Heatmaps)) |
	(1 << u16(ports.MD_Channel.VPVR)) |
	(1 << u16(ports.MD_Channel.Candles)) |
	(1 << u16(ports.MD_Channel.Evidence)) |
	(1 << u16(ports.MD_Channel.Signals)) |
	// S98: Analytics subscription channels.
	(1 << u16(ports.MD_Channel.Analytics_CVD)) |
	(1 << u16(ports.MD_Channel.Analytics_Delta_Volume)) |
	(1 << u16(ports.MD_Channel.Analytics_OI)) |
	(1 << u16(ports.MD_Channel.Analytics_Bar_Stats))
)

endpoint_normalize_venue :: proc(v: string) -> string {
	if len(v) == 0 do return ""
	if util.has_prefix_ci(v, "binance") do return "binance"
	if util.has_prefix_ci(v, "bybit") do return "bybit"
	if util.has_prefix_ci(v, "coinbase") do return "coinbase"
	if util.has_prefix_ci(v, "kraken") do return "kraken"
	if util.has_prefix_ci(v, "hyperliquid") do return "hyperliquid"
	if dash := strings.index(v, "-"); dash > 0 {
		return v[:dash]
	}
	return v
}

endpoint_for_venue :: proc(venue: string) -> Stream_Endpoint {
	v := endpoint_normalize_venue(venue)
	switch v {
	case "binance":
		return Stream_Endpoint{id = .Binance, venue = v, capabilities = {channel_mask = ENDPOINT_ALL_CHANNELS}}
	case "bybit":
		return Stream_Endpoint{id = .Bybit, venue = v, capabilities = {channel_mask = ENDPOINT_ALL_CHANNELS}}
	case "coinbase":
		return Stream_Endpoint{id = .Coinbase, venue = v, capabilities = {channel_mask = ENDPOINT_ALL_CHANNELS}}
	case "kraken":
		return Stream_Endpoint{id = .Kraken, venue = v, capabilities = {channel_mask = ENDPOINT_ALL_CHANNELS}}
	case "hyperliquid":
		return Stream_Endpoint{id = .Hyperliquid, venue = v, capabilities = {channel_mask = ENDPOINT_ALL_CHANNELS}}
	}
	return Stream_Endpoint{id = .Unknown, venue = v, capabilities = {channel_mask = ENDPOINT_ALL_CHANNELS}}
}

endpoint_supports_channel :: proc(endpoint: Stream_Endpoint, channel: ports.MD_Channel) -> bool {
	mask := u16(1 << u16(channel))
	return (endpoint.capabilities.channel_mask & mask) != 0
}

endpoint_build_stream_id_into :: proc(buf: []u8, venue: string, symbol: string) -> string {
	normalized := endpoint_normalize_venue(venue)
	return format_stream_id_into(buf, normalized, symbol, "")
}
