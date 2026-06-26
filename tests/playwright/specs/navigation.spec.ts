/**
 * Page Navigation — validates route transitions across all 6 pages.
 *
 * Route enum: Dashboard(0), Markets(1), Settings(2),
 *   Instrument_Overview(3), Session_Health(4), Portfolio(5)
 *
 * Each route transition invokes on_leave → close_all_overlays → on_enter.
 * We verify that navigation succeeds without errors and that returning
 * to the dashboard restores data flow.
 */

import { test, expect } from '../fixtures/mr-test';
import { waitForCandles } from '../helpers/wait';

test.describe('Page Navigation', () => {
  test.beforeEach(async ({ dash }) => {
    await dash.bootWithData();
  });

  test('navigate to Markets and back', async ({ dash, probe, console: cons }) => {
    await dash.goToMarkets();
    await dash.screenshot('page-markets');
    // Markets page should render without errors
    expect(cons.errors).toHaveLength(0);

    // Return to dashboard
    await dash.goToDashboard();
    await dash.page.waitForTimeout(2_000);
    // Data should still be flowing
    expect(await probe.hasActiveStream()).toBe(true);
  });

  test('navigate to Settings and back', async ({ dash, probe, console: cons }) => {
    await dash.goToSettings();
    await dash.screenshot('page-settings');
    expect(cons.errors).toHaveLength(0);

    await dash.goToDashboard();
    await dash.page.waitForTimeout(2_000);
    expect(await probe.hasActiveStream()).toBe(true);
  });

  test('navigate to Instrument Overview', async ({ dash, console: cons }) => {
    await dash.goToInstrumentOverview();
    await dash.screenshot('page-instrument-overview');
    expect(cons.errors).toHaveLength(0);
  });

  test('navigate to Session Health', async ({ dash, console: cons }) => {
    await dash.goToSessionHealth();
    await dash.screenshot('page-session-health');
    expect(cons.errors).toHaveLength(0);
  });

  test('rapid page cycling does not crash', async ({ dash, probe, console: cons }) => {
    await dash.goToMarkets();
    await dash.goToSettings();
    await dash.goToMarkets();
    await dash.goToDashboard();
    await dash.page.waitForTimeout(2_000);

    expect(cons.errors).toHaveLength(0);
    expect(await probe.hasActiveStream()).toBe(true);
  });

  test('active route persists to localStorage', async ({ dash }) => {
    await dash.goToMarkets();
    await dash.page.waitForTimeout(1_000);
    const route = await dash.getLocalStorage('mr.settings.active_route');
    // Route should be non-null (Markets = 1)
    expect(route).not.toBeNull();
  });

  test('dashboard restores candle flow after deep navigation', async ({ dash, probe }) => {
    // Navigate away through multiple pages
    await dash.goToMarkets();
    await dash.goToSettings();
    await dash.page.waitForTimeout(2_000);

    // Come back to dashboard
    await dash.goToDashboard();
    await dash.page.waitForTimeout(3_000);

    // Candles should resume
    expect(await probe.candleCount()).toBeGreaterThan(0);
    expect(await probe.hasActiveStream()).toBe(true);
  });
});
