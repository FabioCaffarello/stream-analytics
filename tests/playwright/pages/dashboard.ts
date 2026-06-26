/**
 * DashboardPage — keyboard-driven page object for Stream Analytics.
 *
 * Since the entire UI is rendered to canvas, all interactions are via
 * keyboard shortcuts. This page object encapsulates the keybindings
 * so tests read as behavioral intent, not raw key presses.
 */

import type { Page } from '@playwright/test';
import { waitForFullBoot, waitForCandles, waitForLocalStorage } from '../helpers/wait';
import { WasmProbe } from '../helpers/wasm-probe';

/** Timeframe keys (index matches Odin enum order). */
const TF_KEYS: Record<string, string> = {
  '1m': '1',
  '5s': '2',
  '5m': '3',
  '15m': '4',
  '1h': '5',
  '4h': '6',
  '1d': '7',
};

/** TF index from Odin probe (matches enum order). */
const TF_INDEX: Record<string, number> = {
  '1m': 0,
  '5s': 1,
  '5m': 2,
  '15m': 3,
  '1h': 4,
  '4h': 5,
  '1d': 6,
};

export class DashboardPage {
  private readonly probe: WasmProbe;

  constructor(readonly page: Page) {
    this.probe = new WasmProbe(page);
  }

  // ── Lifecycle ──────────────────────────────────────────────────────

  /** Navigate to `/` and wait for full boot (canvas + WASM + hello + ACK). */
  async boot(): Promise<void> {
    await this.page.goto('/');
    await waitForFullBoot(this.page);
  }

  /** Boot and additionally wait until candle data is flowing. */
  async bootWithData(): Promise<void> {
    await this.boot();
    await waitForCandles(this.page);
  }

  /**
   * Boot with all 7 panels enabled.
   * Pre-seeds localStorage with the full panel visibility mask before navigation
   * so tests that need non-default panels (Trades, Stats, etc.) work correctly.
   * The default layout is Candle+Orderbook only; this overrides it for tests.
   */
  async bootWithAllPanels(): Promise<void> {
    await this.page.addInitScript(() => {
      // "1111111" = all 7 panels visible (Candle,Stats,Counter,Heatmap,VPVR,Trades,Orderbook)
      window.localStorage.setItem('mr.settings.panel_visible_mask', '1111111');
    });
    await this.boot();
    await waitForCandles(this.page);
  }

  // ── Timeframe ──────────────────────────────────────────────────────

  /** Switch to a named timeframe and wait for data to start flowing. */
  async switchTimeframe(tf: string, settleMs = 2_000): Promise<void> {
    const key = TF_KEYS[tf];
    if (!key) throw new Error(`Unknown timeframe: ${tf}. Valid: ${Object.keys(TF_KEYS).join(', ')}`);
    await this.page.keyboard.press(key);
    // Wait for the probe to reflect the new TF index
    const expectedIdx = TF_INDEX[tf];
    await this.page.waitForFunction(
      ([idx]) => {
        const ex = (window as any).__mr_wasm_exports;
        return ex && ex.probe_active_tf_index() === idx;
      },
      [expectedIdx] as const,
      { timeout: 10_000 },
    );
    // Brief settle for rendering
    await this.page.waitForTimeout(settleMs);
  }

  /** Get current active timeframe index from probe. */
  async currentTfIndex(): Promise<number> {
    return this.probe.activeTfIndex();
  }

  // ── Mode toggles ──────────────────────────────────────────────────

  async toggleCompareMode(): Promise<void> {
    await this.page.keyboard.press('c');
    await this.page.waitForTimeout(500);
  }

  async enterCompareMode(): Promise<void> {
    if (!(await this.probe.compareMode())) {
      await this.toggleCompareMode();
    }
  }

  async exitCompareMode(): Promise<void> {
    if (await this.probe.compareMode()) {
      await this.toggleCompareMode();
    }
  }

  async toggleZenMode(): Promise<void> {
    await this.page.keyboard.press('z');
    await this.page.waitForTimeout(500);
  }

  async toggleDetailPanel(): Promise<void> {
    await this.page.keyboard.press('d');
    await this.page.waitForTimeout(500);
  }

  async toggleIndicators(): Promise<void> {
    await this.page.keyboard.press('i');
    await this.page.waitForTimeout(500);
  }

  async toggleHelp(): Promise<void> {
    await this.page.keyboard.press('?');
    await this.page.waitForTimeout(500);
  }

  async openStreamPicker(): Promise<void> {
    await this.page.keyboard.press('p');
    await this.page.waitForTimeout(500);
  }

  async openMarketExplorer(): Promise<void> {
    await this.page.keyboard.press('g');
    await this.page.waitForTimeout(500);
  }

  // ── Stream picker navigation ──────────────────────────────────────

  async pickerSelectNext(): Promise<void> {
    await this.page.keyboard.press('ArrowDown');
    await this.page.waitForTimeout(200);
  }

  async pickerConfirm(): Promise<void> {
    await this.page.keyboard.press('Enter');
    await this.page.waitForTimeout(1_000);
  }

  async pickerDismiss(): Promise<void> {
    await this.page.keyboard.press('Escape');
    await this.page.waitForTimeout(300);
  }

  // ── Page navigation (S57 page module) ─────────────────────────────

  async navigateToPage(key: string): Promise<void> {
    await this.page.keyboard.press(key);
    await this.page.waitForTimeout(1_000);
  }

  // ── Screenshots ───────────────────────────────────────────────────

  async screenshot(label: string): Promise<void> {
    await this.page.screenshot({
      path: `tests/playwright/artifacts/${label}.png`,
      fullPage: true,
    });
  }

  // ── Focus mode ─────────────────────────────────────────────────────

  async toggleFocusMode(): Promise<void> {
    await this.page.keyboard.press('f');
    await this.page.waitForTimeout(500);
  }

  async exitFocusMode(): Promise<void> {
    await this.page.keyboard.press('Escape');
    await this.page.waitForTimeout(500);
  }

  // ── Page navigation (Route enum: Dashboard=0, Markets=1, Settings=2,
  //    Instrument_Overview=3, Session_Health=4, Portfolio=5) ─────────

  async goToDashboard(): Promise<void> {
    // Escape exits overlays, then navigate — dashboard is default
    await this.page.keyboard.press('Escape');
    await this.page.waitForTimeout(300);
  }

  async goToMarkets(): Promise<void> {
    await this.page.keyboard.press('m');
    await this.page.waitForTimeout(1_000);
  }

  async goToSettings(): Promise<void> {
    await this.page.keyboard.press('s');
    await this.page.waitForTimeout(1_000);
  }

  async goToInstrumentOverview(): Promise<void> {
    await this.page.keyboard.press('i');
    await this.page.waitForTimeout(1_000);
  }

  async goToSessionHealth(): Promise<void> {
    // Session health is typically accessed via the health page shortcut
    await this.page.keyboard.press('h');
    await this.page.waitForTimeout(1_000);
  }

  // ── Indicator individual toggles ──────────────────────────────────
  // These use the indicator pill keyboard shortcuts when indicator panel is open.
  // The panel must be open first (toggleIndicators), then number keys toggle individual ones.

  /**
   * Toggle a specific indicator by pressing its pill index key.
   * Indicator pills: 0=MA, 1=BBands, 2=VWAP, 3=RSI, 4=MACD, 5=Funding, 6=Liq, 7=Trade_Counter
   * Analytics pills: 8=CVD, 9=DeltaVol, 10=OI (accessed via indicator panel)
   */
  async toggleIndicatorByIndex(idx: number): Promise<void> {
    // When indicator panel is shown, clicking on pill toggles it
    // In keyboard mode, the number keys may map to indicators
    // For now we use the probe-verified approach via settings
    await this.page.keyboard.press(String(idx));
    await this.page.waitForTimeout(500);
  }

  // ── Resync ────────────────────────────────────────────────────────

  async triggerResync(): Promise<void> {
    await this.page.keyboard.press('r');
    await this.page.waitForTimeout(2_000);
  }

  // ── localStorage access ───────────────────────────────────────────

  async getLocalStorage(key: string): Promise<string | null> {
    return this.page.evaluate((k) => localStorage.getItem(k), key);
  }

  async setLocalStorage(key: string, value: string): Promise<void> {
    await this.page.evaluate(([k, v]) => localStorage.setItem(k, v), [key, value] as const);
  }

  async clearLocalStorage(): Promise<void> {
    await this.page.evaluate(() => localStorage.clear());
  }

  /** Get all mr.settings.* keys from localStorage as an object. */
  async getWorkspaceSettings(): Promise<Record<string, string>> {
    return this.page.evaluate(() => {
      const result: Record<string, string> = {};
      for (let i = 0; i < localStorage.length; i++) {
        const key = localStorage.key(i);
        if (key && key.startsWith('mr.settings.')) {
          result[key] = localStorage.getItem(key) || '';
        }
      }
      return result;
    });
  }

  // ── Probe access ──────────────────────────────────────────────────

  get probes(): WasmProbe {
    return this.probe;
  }
}
