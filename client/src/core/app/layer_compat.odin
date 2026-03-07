package app

import "mr:layers"

// Legacy widget slot -> layer bundle compatibility mapping.
// This preserves current layouts while rendering through the new layer canvas.
// Route tags (high bits) keep legacy channel intent without relying on
// Widget_Kind switches in hot-path reconciliation.
LEGACY_ROUTE_CANDLE    :: u32(1 << 24)
LEGACY_ROUTE_TRADES    :: u32(1 << 25)
LEGACY_ROUTE_ORDERBOOK :: u32(1 << 26)
LEGACY_ROUTE_DOM       :: u32(1 << 27)
LEGACY_ROUTE_HEATMAP   :: u32(1 << 28)
LEGACY_ROUTE_VPVR      :: u32(1 << 29)
LEGACY_ROUTE_STATS     :: u32(1 << 30)
LEGACY_ROUTE_COUNTER   :: u32(1 << 31)

legacy_widget_bundle :: proc(kind: Widget_Kind) -> u32 {
	switch kind {
	case .Candle:
		return u32(layers.Layer_Bundle.Bundle_Candles) | LEGACY_ROUTE_CANDLE
	case .Trades:
		return u32(layers.Layer_Bundle.Bundle_Trades) | LEGACY_ROUTE_TRADES
	case .Orderbook:
		return u32(layers.Layer_Bundle.Bundle_Orderbook) | LEGACY_ROUTE_ORDERBOOK
	case .DOM:
		return u32(layers.Layer_Bundle.Bundle_DOM) | LEGACY_ROUTE_DOM
	case .Heatmap:
		return u32(layers.Layer_Bundle.Bundle_Heatmap) | LEGACY_ROUTE_HEATMAP
	case .VPVR:
		return u32(layers.Layer_Bundle.Bundle_VPVR) | LEGACY_ROUTE_VPVR
	case .Stats:
		return u32(layers.Layer_Bundle.Bundle_Stats) | LEGACY_ROUTE_STATS
	case .Counter:
		return u32(layers.Layer_Bundle.Bundle_Counter) | LEGACY_ROUTE_COUNTER
	case .Analytics:
		// S48: Analytics widgets render directly from cell stores, no layer canvas needed.
		return u32(layers.Layer_Bundle.Bundle_Empty)
	case .Session_VPVR, .TPO:
		// S49: Session profile widgets render directly from cell stores.
		return u32(layers.Layer_Bundle.Bundle_Empty)
	case .Empty:
		return u32(layers.Layer_Bundle.Bundle_Empty)
	}
	return 0
}
