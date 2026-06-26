/**
 * WasmProbe — typed access to `window.__mr_wasm_exports` probe functions.
 *
 * Every `probe_*` export from Odin WASM is auto-discovered at runtime.
 * This helper provides a typed façade so tests don't evaluate raw JS strings.
 */

import type { Page } from '@playwright/test';

/** Snapshot of all probe values returned by `window.__mr_widget_probe()`. */
export type ProbeSnapshot = Record<string, number>;

export class WasmProbe {
  constructor(private readonly page: Page) {}

  /** Wait until the WASM module has loaded and probe hooks are installed. */
  async waitForReady(timeoutMs = 15_000): Promise<void> {
    await this.page.waitForFunction(
      () => typeof (window as any).__mr_wasm_exports !== 'undefined',
      { timeout: timeoutMs },
    );
  }

  /** Return a full snapshot of all probes (camelCase keys). */
  async snapshot(): Promise<ProbeSnapshot> {
    return this.page.evaluate(() => (window as any).__mr_widget_probe());
  }

  /** Read a single probe by its Odin export name (e.g. `probe_widget_candle_count`). */
  async read(probeName: string): Promise<number> {
    return this.page.evaluate(
      (name) => {
        const ex = (window as any).__mr_wasm_exports;
        if (!ex || typeof ex[name] !== 'function') return -1;
        return ex[name]();
      },
      probeName,
    );
  }

  // ── Convenience accessors ──────────────────────────────────────────

  async candleCount(): Promise<number> {
    return this.read('probe_widget_candle_count');
  }

  async tradesCount(): Promise<number> {
    return this.read('probe_widget_trades_count');
  }

  async hasActiveStream(): Promise<boolean> {
    return (await this.read('probe_has_active_stream')) === 1;
  }

  async activeTfIndex(): Promise<number> {
    return this.read('probe_active_tf_index');
  }

  async compareMode(): Promise<boolean> {
    return (await this.read('probe_compare_mode')) === 1;
  }

  async subscribeAckCount(): Promise<number> {
    return this.read('probe_md_subscribe_ack_count');
  }

  async helloReceived(): Promise<boolean> {
    return (await this.read('probe_md_hello_received')) === 1;
  }

  async resyncCount(): Promise<number> {
    return this.read('probe_md_resync_count');
  }

  async seqGapCount(): Promise<number> {
    return this.read('probe_md_seq_gap_count');
  }

  async streamSwitchesTotal(): Promise<number> {
    return this.read('probe_stream_switches_total');
  }

  async timeframeSwitchesTotal(): Promise<number> {
    return this.read('probe_timeframe_switches_total');
  }

  // ── Widget data probes ───────────────────────────────────────────

  async orderbookAsks(): Promise<number> {
    return this.read('probe_widget_orderbook_asks');
  }

  async orderbookBids(): Promise<number> {
    return this.read('probe_widget_orderbook_bids');
  }

  async statsCount(): Promise<number> {
    return this.read('probe_widget_stats_count');
  }

  async statsState(): Promise<number> {
    return this.read('probe_widget_stats_state');
  }

  async heatmapSnaps(): Promise<number> {
    return this.read('probe_widget_heatmap_snaps');
  }

  async vpvrLevels(): Promise<number> {
    return this.read('probe_widget_vpvr_levels');
  }

  async evidenceCount(): Promise<number> {
    return this.read('probe_widget_evidence_count');
  }

  async signalCount(): Promise<number> {
    return this.read('probe_widget_signal_count');
  }

  async domEntries(): Promise<number> {
    return this.read('probe_widget_dom_entries');
  }

  async tapeEntries(): Promise<number> {
    return this.read('probe_widget_tape_entries');
  }

  // ── Indicator enabled/rendered probes ────────────────────────────

  async indicatorEnabled(name: 'rsi' | 'macd' | 'funding' | 'liq' | 'trade_counter'): Promise<boolean> {
    return (await this.read(`probe_indicator_${name}_enabled`)) === 1;
  }

  async indicatorRendered(name: 'rsi' | 'macd' | 'funding' | 'liq' | 'trade_counter'): Promise<boolean> {
    return (await this.read(`probe_indicator_${name}_rendered`)) === 1;
  }

  // ── Layout / persistence probes ──────────────────────────────────

  async layoutVersion(): Promise<number> {
    return this.read('probe_layout_version');
  }

  async layoutMigrated(): Promise<boolean> {
    return (await this.read('probe_layout_migrated')) === 1;
  }

  // ── Compare pane probes ──────────────────────────────────────────

  async compareCount(): Promise<number> {
    return this.read('probe_compare_count');
  }

  async compareFocusedIdx(): Promise<number> {
    return this.read('probe_compare_widget_idx');
  }

  // ── Display state probes ─────────────────────────────────────────

  async activeLiveStats(): Promise<boolean> {
    return (await this.read('probe_active_live_stats')) === 1;
  }

  async activeLiveCandle(): Promise<boolean> {
    return (await this.read('probe_active_live_candle')) === 1;
  }

  async activeLiveHeatmap(): Promise<boolean> {
    return (await this.read('probe_active_live_heatmap')) === 1;
  }

  async activeLiveVpvr(): Promise<boolean> {
    return (await this.read('probe_active_live_vpvr')) === 1;
  }

  // ── Stream probes ────────────────────────────────────────────────

  async streamCount(): Promise<number> {
    return this.read('probe_stream_count');
  }

  async activeSubjectLo32(): Promise<number> {
    return this.read('probe_active_subject_lo32');
  }

  // ── Performance probes ───────────────────────────────────────────

  async statsRenderP95Us(): Promise<number> {
    return this.read('probe_widget_stats_render_p95_us');
  }

  async statsRenderP99Us(): Promise<number> {
    return this.read('probe_widget_stats_render_p99_us');
  }

  async statsRenderOverBudget(): Promise<number> {
    return this.read('probe_widget_stats_render_over_budget');
  }

  async tapeRenderP95Us(): Promise<number> {
    return this.read('probe_widget_tape_render_p95_us');
  }

  async tapeRenderOverBudget(): Promise<number> {
    return this.read('probe_widget_tape_render_over_budget');
  }

  async domRenderP95Us(): Promise<number> {
    return this.read('probe_widget_dom_render_p95_us');
  }

  async domRenderOverBudget(): Promise<number> {
    return this.read('probe_widget_dom_render_over_budget');
  }

  async mdParseP95Us(): Promise<number> {
    return this.read('probe_md_parse_time_p95_us');
  }

  async mdParseP99Us(): Promise<number> {
    return this.read('probe_md_parse_time_p99_us');
  }

  async mdApplyP95Us(): Promise<number> {
    return this.read('probe_md_apply_time_p95_us');
  }

  async mdApplyP99Us(): Promise<number> {
    return this.read('probe_md_apply_time_p99_us');
  }

  async mdTransportMode(): Promise<number> {
    return this.read('probe_md_transport_mode');
  }

  // ── Backlog probes ───────────────────────────────────────────────

  async tradeBacklog(): Promise<number> {
    return this.read('probe_md_trade_backlog');
  }

  async candleBacklog(): Promise<number> {
    return this.read('probe_md_candle_backlog');
  }

  async signalBacklog(): Promise<number> {
    return this.read('probe_md_signal_backlog');
  }

  // ── Widget drop probes ───────────────────────────────────────────

  async statsDropTotal(): Promise<number> {
    return this.read('probe_widget_stats_drop_total');
  }

  async tapeDropTotal(): Promise<number> {
    return this.read('probe_widget_tape_drop_total');
  }

  async domDropTotal(): Promise<number> {
    return this.read('probe_widget_dom_drop_total');
  }
}
