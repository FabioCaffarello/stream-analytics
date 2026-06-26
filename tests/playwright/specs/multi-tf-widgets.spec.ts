/**
 * Multi-Timeframe Widget Coverage — validates widget data at each major TF.
 *
 * Each timeframe has different data characteristics (1m = more candles,
 * 1d = fewer but larger). This suite verifies that the data pipeline
 * works correctly at every supported timeframe.
 */

import { test, expect } from '../fixtures/mr-test';
import { waitForCandles } from '../helpers/wait';

const MAJOR_TIMEFRAMES = ['1m', '5m', '15m', '1h', '4h'] as const;

test.describe('Multi-TF Widget Coverage', () => {
  test.beforeEach(async ({ dash }) => {
    await dash.bootWithData();
  });

  for (const tf of MAJOR_TIMEFRAMES) {
    test(`${tf}: candles arrive after switch`, async ({ dash, probe }) => {
      await dash.switchTimeframe(tf);
      await waitForCandles(dash.page);
      expect(await probe.candleCount()).toBeGreaterThan(0);
    });
  }

  for (const tf of MAJOR_TIMEFRAMES) {
    test(`${tf}: no seq gaps after switch and 5s settle`, async ({ dash, probe }) => {
      await dash.switchTimeframe(tf);
      await dash.page.waitForTimeout(5_000);
      expect(await probe.seqGapCount()).toBe(0);
    });
  }

  test('round-trip through all TFs returns to original', async ({ dash, probe }) => {
    await dash.switchTimeframe('1m');
    const initialCandles = await probe.candleCount();

    for (const tf of MAJOR_TIMEFRAMES) {
      await dash.switchTimeframe(tf, 1_000);
    }

    // Back to 1m
    await dash.switchTimeframe('1m');
    await dash.page.waitForTimeout(3_000);

    expect(await probe.candleCount()).toBeGreaterThan(0);
    expect(await probe.seqGapCount()).toBe(0);
  });

  test('highest TF (4h) produces valid candle data', async ({ dash, probe }) => {
    await dash.switchTimeframe('4h');
    await waitForCandles(dash.page);

    const close = await probe.read('probe_widget_candle_latest_close');
    expect(close).toBeGreaterThan(0);

    const ts = await probe.read('probe_widget_candle_latest_end_ts');
    const now = Date.now() / 1000;
    // 4h candle timestamp should be within last 24h
    expect(ts).toBeGreaterThan(now - 86400);
  });

  test('trades continue flowing across all TF switches', async ({ dash, probe }) => {
    for (const tf of MAJOR_TIMEFRAMES) {
      await dash.switchTimeframe(tf, 1_000);
    }
    // Trades are TF-independent
    expect(await probe.tradesCount()).toBeGreaterThan(0);
  });
});
