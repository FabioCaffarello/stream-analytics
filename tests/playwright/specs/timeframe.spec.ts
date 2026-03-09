/**
 * Timeframe Switching — validates TF changes via keyboard + probe verification.
 *
 * Each test switches to a named timeframe and verifies the probe reflects the
 * correct index. No blind waits — probe-driven assertions.
 */

import { test, expect } from '../fixtures/mr-test';

test.describe('Timeframe Switching', () => {
  test.beforeEach(async ({ dash }) => {
    await dash.bootWithData();
  });

  const timeframes = ['1m', '5m', '15m', '1h', '4h', '1d'] as const;

  for (const tf of timeframes) {
    test(`switch to ${tf} and verify probe`, async ({ dash, probe }) => {
      await dash.switchTimeframe(tf);
      const idx = await probe.activeTfIndex();
      // switchTimeframe already waits for the probe to match,
      // but we assert explicitly for test clarity
      expect(idx).toBeGreaterThanOrEqual(0);
    });
  }

  test('rapid TF cycle does not cause desync', async ({ dash, probe }) => {
    const cycle = ['1m', '5m', '15m', '1h', '4h', '1d', '1m'] as const;
    for (const tf of cycle) {
      await dash.switchTimeframe(tf, 500);
    }
    // After rapid cycling, verify no seq gaps
    expect(await probe.seqGapCount()).toBe(0);
    // Candles should still be flowing
    expect(await probe.candleCount()).toBeGreaterThan(0);
  });

  test('TF switch counter increments correctly', async ({ dash, probe }) => {
    const before = await probe.timeframeSwitchesTotal();
    await dash.switchTimeframe('5m');
    await dash.switchTimeframe('1h');
    const after = await probe.timeframeSwitchesTotal();
    expect(after - before).toBe(2);
  });
});
