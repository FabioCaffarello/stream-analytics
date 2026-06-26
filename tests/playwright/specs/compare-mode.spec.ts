/**
 * Compare Mode — validates split-pane behavior and TF switching within compare.
 */

import { test, expect } from '../fixtures/mr-test';

test.describe('Compare Mode', () => {
  test.beforeEach(async ({ dash }) => {
    await dash.bootWithData();
  });

  test('enter compare mode via keyboard', async ({ dash, probe }) => {
    await dash.enterCompareMode();
    expect(await probe.compareMode()).toBe(true);
  });

  test('exit compare mode via keyboard', async ({ dash, probe }) => {
    await dash.enterCompareMode();
    await dash.exitCompareMode();
    expect(await probe.compareMode()).toBe(false);
  });

  test('compare mode toggle is idempotent', async ({ dash, probe }) => {
    // Double-enter should still be in compare
    await dash.enterCompareMode();
    await dash.enterCompareMode();
    expect(await probe.compareMode()).toBe(true);

    // Double-exit should still be out
    await dash.exitCompareMode();
    await dash.exitCompareMode();
    expect(await probe.compareMode()).toBe(false);
  });

  test('TF switch works in compare mode', async ({ dash, probe }) => {
    await dash.enterCompareMode();
    await dash.switchTimeframe('5m');
    expect(await probe.compareMode()).toBe(true);
    expect(await probe.candleCount()).toBeGreaterThan(0);
  });

  test('no seq gaps after compare + TF stress', async ({ dash, probe }) => {
    await dash.enterCompareMode();
    await dash.switchTimeframe('5m', 500);
    await dash.switchTimeframe('1h', 500);
    await dash.switchTimeframe('1m', 500);
    await dash.exitCompareMode();
    // Settle
    await dash.page.waitForTimeout(3_000);
    expect(await probe.seqGapCount()).toBe(0);
  });

  test('compare count reflects active panes', async ({ dash, probe }) => {
    await dash.enterCompareMode();
    await dash.page.waitForTimeout(1_000);
    const count = await probe.compareCount();
    // Should have at least 1 compare pane
    expect(count).toBeGreaterThanOrEqual(1);
    await dash.exitCompareMode();
  });

  test('compare focused pane index is valid', async ({ dash, probe }) => {
    await dash.enterCompareMode();
    await dash.page.waitForTimeout(1_000);
    const idx = await probe.compareFocusedIdx();
    expect(idx).toBeGreaterThanOrEqual(0);
    await dash.exitCompareMode();
  });

  test('canvas still renders in compare mode', async ({ dash, canvas }) => {
    await dash.enterCompareMode();
    await dash.page.waitForTimeout(2_000);
    expect(await canvas.hasVisibleContent()).toBe(true);
    await dash.exitCompareMode();
  });

  test('data flows in both normal and compare mode', async ({ dash, probe }) => {
    // Normal mode candles
    const normalCandles = await probe.candleCount();
    expect(normalCandles).toBeGreaterThan(0);

    // Enter compare
    await dash.enterCompareMode();
    await dash.page.waitForTimeout(3_000);
    const compareCandles = await probe.candleCount();
    expect(compareCandles).toBeGreaterThan(0);

    // Exit compare
    await dash.exitCompareMode();
    await dash.page.waitForTimeout(2_000);
    expect(await probe.candleCount()).toBeGreaterThan(0);
  });
});
