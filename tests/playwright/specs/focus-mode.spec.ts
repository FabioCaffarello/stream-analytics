/**
 * Focus Mode — validates the full-screen single-cell focus experience.
 *
 * Focus mode shows a 75/25 candle+orderbook split for the focused cell.
 * It's entered via 'f' and exited via Escape. The focused cell uses its
 * per-cell TF if set, otherwise the global TF.
 */

import { test, expect } from '../fixtures/mr-test';

test.describe('Focus Mode', () => {
  test.beforeEach(async ({ dash }) => {
    await dash.bootWithData();
  });

  test('enter focus mode and canvas still renders', async ({ dash, canvas, console: cons }) => {
    await dash.toggleFocusMode();
    await dash.page.waitForTimeout(1_000);
    expect(await canvas.hasVisibleContent()).toBe(true);
    expect(cons.errors).toHaveLength(0);
  });

  test('exit focus mode via Escape', async ({ dash, probe, console: cons }) => {
    await dash.toggleFocusMode();
    await dash.page.waitForTimeout(500);
    await dash.exitFocusMode();
    await dash.page.waitForTimeout(500);

    // Should be back to normal — candles still flowing
    expect(await probe.candleCount()).toBeGreaterThan(0);
    expect(cons.errors).toHaveLength(0);
  });

  test('data continues flowing in focus mode', async ({ dash, probe }) => {
    await dash.toggleFocusMode();
    const candlesBefore = await probe.candleCount();
    await dash.page.waitForTimeout(5_000);
    const candlesAfter = await probe.candleCount();
    // Candle count should not decrease
    expect(candlesAfter).toBeGreaterThanOrEqual(candlesBefore);
  });

  test('TF switch works in focus mode', async ({ dash, probe }) => {
    await dash.toggleFocusMode();
    await dash.switchTimeframe('15m');
    expect(await probe.activeTfIndex()).toBe(3); // 15m = index 3
    await dash.exitFocusMode();
  });

  test('focus mode round-trip preserves stream', async ({ dash, probe }) => {
    const subjectBefore = await probe.activeSubjectLo32();
    await dash.toggleFocusMode();
    await dash.page.waitForTimeout(1_000);
    await dash.exitFocusMode();
    await dash.page.waitForTimeout(1_000);
    const subjectAfter = await probe.activeSubjectLo32();
    expect(subjectAfter).toBe(subjectBefore);
  });
});
