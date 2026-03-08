package app

import "mr:layers"
import "mr:ports"

// S62: Direct Widget_Kind → channel bitmask mapping.
// Replaces the legacy two-step legacy_widget_bundle() → channels_for_bundle() indirection
// that encoded both Layer_Bundle and route tags into a single u32.

// Channel bitmask for a widget kind — determines which MD channels to subscribe.
channels_for_widget :: proc(kind: Widget_Kind) -> u16 {
	CH_TRADES    :: u16(1 << u16(ports.MD_Channel.Trades))
	CH_ORDERBOOK :: u16(1 << u16(ports.MD_Channel.Orderbook))
	CH_STATS     :: u16(1 << u16(ports.MD_Channel.Stats))
	CH_HEATMAPS  :: u16(1 << u16(ports.MD_Channel.Heatmaps))
	CH_VPVR      :: u16(1 << u16(ports.MD_Channel.VPVR))
	CH_CANDLES   :: u16(1 << u16(ports.MD_Channel.Candles))
	CH_EVIDENCE  :: u16(1 << u16(ports.MD_Channel.Evidence))
	CH_SIGNALS   :: u16(1 << u16(ports.MD_Channel.Signals))
	CH_TAPE      :: u16(1 << u16(ports.MD_Channel.Tape))
	// S98: Analytics subscription channels — each maps to its own NATS subject.
	CH_ANALYTICS :: u16(1 << u16(ports.MD_Channel.Analytics_CVD)) |
	                u16(1 << u16(ports.MD_Channel.Analytics_Delta_Volume)) |
	                u16(1 << u16(ports.MD_Channel.Analytics_OI)) |
	                u16(1 << u16(ports.MD_Channel.Analytics_Bar_Stats))

	switch kind {
	case .Candle:
		// S98: Include analytics channels for subplot support (CVD/DV/OI below chart).
		return CH_CANDLES | CH_STATS | CH_HEATMAPS | CH_VPVR | CH_EVIDENCE | CH_SIGNALS | CH_ANALYTICS
	case .Orderbook:
		return CH_ORDERBOOK
	case .DOM:
		return CH_ORDERBOOK | CH_TRADES
	case .Trades:
		return CH_TRADES | CH_TAPE
	case .Stats:
		return CH_STATS
	case .Counter:
		return CH_CANDLES | CH_STATS
	case .Heatmap:
		return CH_HEATMAPS
	case .VPVR:
		return CH_VPVR
	case .Analytics:
		// S98: Subscribe to dedicated analytics channels (replaces S81 candle piggyback).
		return CH_ANALYTICS
	case .Session_VPVR, .TPO, .Empty:
		return 0
	}
	return 0
}

// Layer bundle for a widget kind — determines which layers to render.
layer_bundle_for_widget :: proc(kind: Widget_Kind) -> u32 {
	switch kind {
	case .Candle:
		return u32(layers.Layer_Bundle.Bundle_Candles)
	case .Trades:
		return u32(layers.Layer_Bundle.Bundle_Trades)
	case .Orderbook:
		return u32(layers.Layer_Bundle.Bundle_Orderbook)
	case .DOM:
		return u32(layers.Layer_Bundle.Bundle_DOM)
	case .Heatmap:
		return u32(layers.Layer_Bundle.Bundle_Heatmap)
	case .VPVR:
		return u32(layers.Layer_Bundle.Bundle_VPVR)
	case .Stats:
		return u32(layers.Layer_Bundle.Bundle_Stats)
	case .Counter:
		return u32(layers.Layer_Bundle.Bundle_Counter)
	case .Analytics:
		return u32(layers.Layer_Bundle.Bundle_Analytics)
	case .Session_VPVR, .TPO, .Empty:
		return u32(layers.Layer_Bundle.Bundle_Empty)
	}
	return 0
}

// Compare mode: widget kind for a compare pane index.
compare_widget_kind_for_idx :: proc(widget_idx: int) -> Widget_Kind {
	switch widget_idx {
	case 0: return .Orderbook
	case 1: return .Trades
	case 2: return .Candle
	case 3: return .Analytics // S84: analytics compare mode
	}
	return .Candle
}
