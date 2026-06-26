package app

// S157: Orderflow Vertical Slice I — integration tests.
// Covers: store resolution, readiness, TF-change footprint clearing, contract routing.

import "core:testing"
import "mr:layers"
import "mr:md_common"
import "mr:ports"
import "mr:services"

// Helper: apply a trade event to create a stream and populate trade-derived stores.
@(private = "file")
seed_trade_stream :: proc(store: ^layers.Market_Store, sid: u64, price: f64, qty: f64, is_buy: bool) {
	evt := ports.MD_Event{
		source = {subject_id = sid, channel = .Trades, seq = 1},
		kind   = .Trade,
		unix   = 60_000,
	}
	evt.data.trade = ports.MD_Trade_Event{price = price, qty = qty, is_buy = is_buy, unix = 60_000}
	layers.market_store_apply_event(store, &evt)
}

// ---------------------------------------------------------------------------
// resolve_stores_for_cell: DOM/Footprint wiring for follow-active cells
// ---------------------------------------------------------------------------

@(test)
test_s157_resolve_stores_includes_dom_follow_active :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.world.count = 1

	sid := u64(157_001)
	// Apply a trade → creates stream + populates trades/dom stores.
	seed_trade_stream(&state.layer_store, sid, 100.0, 1.0, true)
	state.layer_store.active_subject_id = sid

	stream := layers.market_store_active_stream(&state.layer_store)
	testing.expect(t, stream != nil, "active stream should exist")

	state.world.bindings[0].stream_idx = -1

	stores := resolve_stores_for_cell(state, 0)
	testing.expect(t, stores.dom != nil, "DOM store should be wired for follow-active cell")
	testing.expect(t, services.dom_store_has_data(stores.dom), "DOM should have data")
}

@(test)
test_s157_resolve_stores_includes_footprint_follow_active :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.world.count = 1

	sid := u64(157_002)
	state.layer_store.active_tf_ms = 60_000 // 1m TF required for footprint
	seed_trade_stream(&state.layer_store, sid, 100.0, 1.0, true)
	state.layer_store.active_subject_id = sid

	stream := layers.market_store_active_stream(&state.layer_store)
	testing.expect(t, stream != nil, "active stream should exist")
	testing.expect(t, stream.footprint.count > 0, "footprint should have been populated by trade reducer")

	state.world.bindings[0].stream_idx = -1

	stores := resolve_stores_for_cell(state, 0)
	testing.expect(t, stores.footprint != nil, "Footprint store should be wired for follow-active cell")
	testing.expect(t, stores.footprint.count > 0, "Footprint should have data")
}

// ---------------------------------------------------------------------------
// Footprint readiness
// ---------------------------------------------------------------------------

@(test)
test_s157_footprint_readiness_no_data :: proc(t: ^testing.T) {
	stores: Cell_Stores
	fp: services.Footprint_Store
	stores.footprint = &fp

	has := widget_store_has_data(.Footprint, stores)
	testing.expect(t, !has, "Footprint with no data should not be renderable")
}

@(test)
test_s157_footprint_readiness_with_data :: proc(t: ^testing.T) {
	stores: Cell_Stores
	fp: services.Footprint_Store
	services.footprint_store_push_trade(&fp, 100.0, 1.0, true, 60_000, 60_000, 1.0)
	stores.footprint = &fp

	has := widget_store_has_data(.Footprint, stores)
	testing.expect(t, has, "Footprint with data should be renderable")
}

@(test)
test_s157_footprint_readiness_nil_store :: proc(t: ^testing.T) {
	stores: Cell_Stores
	has := widget_store_has_data(.Footprint, stores)
	testing.expect(t, !has, "Footprint with nil store should not be renderable")
}

// ---------------------------------------------------------------------------
// DOM readiness via widget_store_has_data
// ---------------------------------------------------------------------------

@(test)
test_s157_dom_readiness_orderbook_only :: proc(t: ^testing.T) {
	stores: Cell_Stores
	ob: services.Orderbook_Store
	ob.bid_count = 5
	stores.orderbook = &ob

	has := widget_store_has_data(.DOM, stores)
	testing.expect(t, has, "DOM should be renderable with orderbook data alone")
}

@(test)
test_s157_dom_readiness_fills_only :: proc(t: ^testing.T) {
	stores: Cell_Stores
	dom: services.DOM_Store
	services.dom_store_push_trade(&dom, 100.0, 1.0, true, 1000, 1.0)
	stores.dom = &dom

	has := widget_store_has_data(.DOM, stores)
	testing.expect(t, has, "DOM should be renderable with fills alone (no OB)")
}

@(test)
test_s157_dom_readiness_both :: proc(t: ^testing.T) {
	stores: Cell_Stores
	ob: services.Orderbook_Store
	ob.bid_count = 5
	dom: services.DOM_Store
	services.dom_store_push_trade(&dom, 100.0, 1.0, true, 1000, 1.0)
	stores.orderbook = &ob
	stores.dom = &dom

	has := widget_store_has_data(.DOM, stores)
	testing.expect(t, has, "DOM should be renderable with both OB and fills")
}

@(test)
test_s157_dom_readiness_empty :: proc(t: ^testing.T) {
	stores: Cell_Stores
	ob: services.Orderbook_Store
	dom: services.DOM_Store
	stores.orderbook = &ob
	stores.dom = &dom

	has := widget_store_has_data(.DOM, stores)
	testing.expect(t, !has, "DOM with empty stores should not be renderable")
}

// ---------------------------------------------------------------------------
// Footprint contract routing
// ---------------------------------------------------------------------------

@(test)
test_s157_footprint_contract_has_renderer :: proc(t: ^testing.T) {
	contract := WIDGET_CONTRACTS[Widget_Kind.Footprint]
	testing.expect(t, contract.on_render != nil, "Footprint should have a render proc")
	testing.expect(t, contract.on_create != nil, "Footprint should have a create proc")
}

@(test)
test_s157_footprint_contract_not_empty :: proc(t: ^testing.T) {
	fp_render := WIDGET_CONTRACTS[Widget_Kind.Footprint].on_render
	empty_render := WIDGET_CONTRACTS[Widget_Kind.Empty].on_render
	testing.expect(t, fp_render != empty_render, "Footprint should NOT use empty contract renderer")
}

// ---------------------------------------------------------------------------
// Footprint readiness policy
// ---------------------------------------------------------------------------

@(test)
test_s157_footprint_readiness_policy :: proc(t: ^testing.T) {
	policy := widget_readiness_policy(.Footprint)
	testing.expect_value(t, policy.primary_artifact, md_common.Artifact_Kind.Trade)
	testing.expect(t, policy.partial_usable, "Footprint should be partial_usable")
	testing.expect(t, policy.backfill_absent_usable, "Footprint should be usable without backfill")
	testing.expect(t, policy.uses_artifact_live_flag, "Footprint should track per-artifact live flag")
}

// ---------------------------------------------------------------------------
// Footprint TF-change clearing
// ---------------------------------------------------------------------------

@(test)
test_s157_footprint_cleared_on_tf_change :: proc(t: ^testing.T) {
	store: services.Footprint_Store
	services.footprint_store_push_trade(&store, 100.0, 1.0, true, 60_000, 60_000, 1.0)
	services.footprint_store_push_trade(&store, 101.0, 2.0, false, 120_000, 60_000, 1.0)
	testing.expect_value(t, store.count, 2)

	services.footprint_store_reset(&store)
	testing.expect_value(t, store.count, 0)
	testing.expect_value(t, store.head, 0)
}

// ---------------------------------------------------------------------------
// Trades readiness
// ---------------------------------------------------------------------------

@(test)
test_s157_trades_readiness_with_data :: proc(t: ^testing.T) {
	stores: Cell_Stores
	tr: services.Trades_Store
	services.push_trade(&tr, services.Trade_Entry{price = 100.0, qty = 1.0, side = .Buy, unix = 1000})
	stores.trades = &tr

	has := widget_store_has_data(.Trades, stores)
	testing.expect(t, has, "Trades should be renderable with data")
}

@(test)
test_s157_trades_readiness_empty :: proc(t: ^testing.T) {
	stores: Cell_Stores
	tr: services.Trades_Store
	stores.trades = &tr

	has := widget_store_has_data(.Trades, stores)
	testing.expect(t, !has, "Trades with empty store should not be renderable")
}

// ---------------------------------------------------------------------------
// resolve_stores_for_pane consistency
// ---------------------------------------------------------------------------

@(test)
test_s157_resolve_stores_for_pane_includes_dom :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)

	sid := u64(157_010)
	seed_trade_stream(&state.layer_store, sid, 100.0, 1.0, true)
	state.layer_store.active_subject_id = sid

	binding: Stream_Binding
	binding.stream_idx = -1
	stores := resolve_stores_for_pane(state, &binding, 2)
	testing.expect(t, stores.dom != nil, "resolve_stores_for_pane should include DOM")
	testing.expect(t, services.dom_store_has_data(stores.dom), "DOM should have data via pane path")
}

@(test)
test_s157_resolve_stores_for_pane_includes_footprint :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)

	sid := u64(157_011)
	state.layer_store.active_tf_ms = 60_000
	seed_trade_stream(&state.layer_store, sid, 100.0, 1.0, true)
	state.layer_store.active_subject_id = sid

	binding: Stream_Binding
	binding.stream_idx = -1
	stores := resolve_stores_for_pane(state, &binding, 2)
	testing.expect(t, stores.footprint != nil, "resolve_stores_for_pane should include footprint")
	testing.expect(t, stores.footprint.count > 0, "Footprint should have data via pane path")
}
