package services

// In-memory settings store with dirty tracking.
// Fixed capacity, zero allocation. Platform port handles persistence.

import "mr:ports"

SETTINGS_CAP :: 128

Settings_Entry :: struct {
	key:   string,
	value: string,
	used:  bool,
}

Settings_Store :: struct {
	entries: [SETTINGS_CAP]Settings_Entry,
	count:   int,
	dirty:   bool,
	port:    ports.Settings_Port,
}

// Known settings keys.
SETTING_ACTIVE_TF_IDX             :: "active_tf_idx"
SETTING_OB_PRICE_GRP :: "ob_price_group"
SETTING_ACTIVE_STREAM_SUBJECT_ID :: "active_stream_subject_id"
SETTING_ACTIVE_STREAM_VENUE      :: "active_stream_venue"
SETTING_ACTIVE_STREAM_SYMBOL     :: "active_stream_symbol"
SETTING_ACTIVE_STREAM_CHANNEL    :: "active_stream_channel"
SETTING_SIDEBAR_EXPANDED         :: "sidebar_expanded"
SETTING_OB_GROUP_IDX             :: "ob_group_idx"
SETTING_TRADE_FILTER_IDX         :: "trade_filter_idx"
SETTING_SHOW_CANDLE_VOL          :: "show_candle_vol"
SETTING_SHOW_CANDLE_HEATMAP      :: "show_candle_heatmap"
SETTING_SHOW_CANDLE_VPVR         :: "show_candle_vpvr"
SETTING_CANDLE_HEATMAP_INTENSITY_IDX :: "candle_heatmap_intensity_idx"
SETTING_PANEL_VISIBLE_MASK       :: "panel_visible_mask"
SETTING_LAYOUT                   :: "layout"
SETTING_LAYOUT_PRESET            :: "layout_preset"
SETTING_CUSTOM_LAYOUT_0          :: "custom_layout_0"
SETTING_CUSTOM_LAYOUT_1          :: "custom_layout_1"
SETTING_CUSTOM_LAYOUT_2          :: "custom_layout_2"
SETTING_CUSTOM_LAYOUT_3          :: "custom_layout_3"
SETTING_SHOW_MA                  :: "show_ma"
SETTING_SHOW_BBANDS              :: "show_bbands"
SETTING_SHOW_VWAP                :: "show_vwap"
SETTING_SHOW_RSI                 :: "show_rsi"
SETTING_SHOW_MACD                :: "show_macd"
SETTING_SHOW_FUNDING             :: "show_funding"
SETTING_SHOW_LIQ                 :: "show_liq"
SETTING_SHOW_TRADE_COUNTER       :: "show_trade_counter"
SETTING_DRAW_TOOLS               :: "draw_tools"
SETTING_LAYOUT_V2                :: "layout_v2"
SETTING_MA_PERIOD_0              :: "ma_period_0"
SETTING_MA_PERIOD_1              :: "ma_period_1"
SETTING_MA_PERIOD_2              :: "ma_period_2"
SETTING_BB_PERIOD                :: "bb_period"
SETTING_BB_SIGMA                 :: "bb_sigma"
SETTING_RSI_PERIOD               :: "rsi_period"
SETTING_MACD_FAST                :: "macd_fast"
SETTING_MACD_SLOW                :: "macd_slow"
SETTING_MACD_SIGNAL              :: "macd_signal"
SETTING_ROW_WEIGHTS              :: "row_weights"
SETTING_COL_WEIGHTS              :: "col_weights"
SETTING_LAYOUT_V3                :: "layout_v3"
SETTING_LAYOUT_V4                :: "layout_v4"
SETTING_LAYOUT_MODE              :: "layout_mode"
SETTING_CONNECTION_PROFILE_COUNT :: "connection_profile_count"
SETTING_CONNECTION_PROFILE_ACTIVE :: "connection_profile_active"
SETTING_CONNECTION_PROFILE_0     :: "connection_profile_0"
SETTING_CONNECTION_PROFILE_1     :: "connection_profile_1"
SETTING_CONNECTION_PROFILE_2     :: "connection_profile_2"
SETTING_CONNECTION_PROFILE_3     :: "connection_profile_3"
SETTING_CONNECTION_PROFILE_4     :: "connection_profile_4"
SETTING_CONNECTION_PROFILE_5     :: "connection_profile_5"
SETTING_CONNECTION_PROFILE_6     :: "connection_profile_6"
SETTING_CONNECTION_PROFILE_7     :: "connection_profile_7"
SETTING_CONNECTION_PROFILE_8     :: "connection_profile_8"
SETTING_CONNECTION_PROFILE_9     :: "connection_profile_9"
SETTING_CONNECTION_PROFILE_10    :: "connection_profile_10"
SETTING_CONNECTION_PROFILE_11    :: "connection_profile_11"

// Initialize store, loading known keys from port.
settings_init :: proc(store: ^Settings_Store, port: ports.Settings_Port) {
	store.port = port
	if port.load == nil do return

	// Pre-load known keys.
	known_keys := [?]string{
		SETTING_ACTIVE_TF_IDX, SETTING_OB_PRICE_GRP,
			SETTING_ACTIVE_STREAM_SUBJECT_ID,
			SETTING_ACTIVE_STREAM_VENUE, SETTING_ACTIVE_STREAM_SYMBOL, SETTING_ACTIVE_STREAM_CHANNEL,
				SETTING_SIDEBAR_EXPANDED, SETTING_OB_GROUP_IDX, SETTING_TRADE_FILTER_IDX, SETTING_SHOW_CANDLE_VOL,
				SETTING_SHOW_CANDLE_HEATMAP, SETTING_SHOW_CANDLE_VPVR, SETTING_CANDLE_HEATMAP_INTENSITY_IDX,
				SETTING_PANEL_VISIBLE_MASK,
				SETTING_LAYOUT, SETTING_LAYOUT_V2, SETTING_LAYOUT_PRESET,
				SETTING_CUSTOM_LAYOUT_0, SETTING_CUSTOM_LAYOUT_1,
				SETTING_CUSTOM_LAYOUT_2, SETTING_CUSTOM_LAYOUT_3,
				SETTING_SHOW_MA, SETTING_SHOW_BBANDS, SETTING_SHOW_VWAP,
				SETTING_SHOW_RSI, SETTING_SHOW_MACD,
				SETTING_SHOW_FUNDING, SETTING_SHOW_LIQ,
				SETTING_SHOW_TRADE_COUNTER, SETTING_DRAW_TOOLS,
				SETTING_ROW_WEIGHTS, SETTING_COL_WEIGHTS,
				SETTING_LAYOUT_V3, SETTING_LAYOUT_V4, SETTING_LAYOUT_MODE,
				SETTING_CONNECTION_PROFILE_COUNT, SETTING_CONNECTION_PROFILE_ACTIVE,
				SETTING_CONNECTION_PROFILE_0, SETTING_CONNECTION_PROFILE_1,
				SETTING_CONNECTION_PROFILE_2, SETTING_CONNECTION_PROFILE_3,
				SETTING_CONNECTION_PROFILE_4, SETTING_CONNECTION_PROFILE_5,
				SETTING_CONNECTION_PROFILE_6, SETTING_CONNECTION_PROFILE_7,
				SETTING_CONNECTION_PROFILE_8, SETTING_CONNECTION_PROFILE_9,
				SETTING_CONNECTION_PROFILE_10, SETTING_CONNECTION_PROFILE_11,
			}
	for key in known_keys {
		value, ok := port.load(key)
		if ok {
			settings_set_internal(store, key, value)
		}
	}
	store.dirty = false // loading doesn't count as dirty
}

settings_get :: proc(store: ^Settings_Store, key: string) -> (string, bool) {
	for i in 0 ..< store.count {
		if store.entries[i].used && store.entries[i].key == key {
			return store.entries[i].value, true
		}
	}
	return "", false
}

settings_set :: proc(store: ^Settings_Store, key: string, value: string) {
	settings_set_internal(store, key, value)
	store.dirty = true
}

settings_flush :: proc(store: ^Settings_Store) {
	if !store.dirty do return
	if store.port.save == nil do return

	for i in 0 ..< store.count {
		if store.entries[i].used {
			store.port.save(store.entries[i].key, store.entries[i].value)
		}
	}

	if store.port.flush != nil {
		store.port.flush()
	}
	store.dirty = false
}

// --- Internal ---

@(private = "file")
settings_set_internal :: proc(store: ^Settings_Store, key: string, value: string) {
	// Update existing.
	for i in 0 ..< store.count {
		if store.entries[i].used && store.entries[i].key == key {
			store.entries[i].value = value
			return
		}
	}
	// Insert new.
	if store.count < SETTINGS_CAP {
		store.entries[store.count] = Settings_Entry{
			key   = key,
			value = value,
			used  = true,
		}
		store.count += 1
	}
}
