/**
 * wait — probe-based waiting strategies that replace blind setTimeout.
 *
 * Instead of `page.waitForTimeout(8_000)` we poll WASM probes until the
 * expected state is reached, with a hard timeout as safety net.
 */

import type { Page } from '@playwright/test';

/** Poll a condition every `intervalMs` until it returns true or `timeoutMs` expires. */
export async function waitUntil(
  page: Page,
  conditionFn: () => Promise<boolean>,
  { timeoutMs = 30_000, intervalMs = 500, label = 'condition' } = {},
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (await conditionFn()) return;
    await page.waitForTimeout(intervalMs);
  }
  throw new Error(`waitUntil timed out after ${timeoutMs}ms waiting for: ${label}`);
}

/** Wait until WASM probe `probe_has_active_stream` returns 1. */
export async function waitForActiveStream(page: Page, timeoutMs = 30_000): Promise<void> {
  await page.waitForFunction(
    () => {
      const ex = (window as any).__mr_wasm_exports;
      return ex && typeof ex.probe_has_active_stream === 'function' && ex.probe_has_active_stream() === 1;
    },
    { timeout: timeoutMs },
  );
}

/** Wait until WASM probe `probe_md_subscribe_ack_count` > 0 (server acknowledged subscription). */
export async function waitForSubscribeAck(page: Page, timeoutMs = 30_000): Promise<void> {
  await page.waitForFunction(
    () => {
      const ex = (window as any).__mr_wasm_exports;
      return ex && typeof ex.probe_md_subscribe_ack_count === 'function' && ex.probe_md_subscribe_ack_count() > 0;
    },
    { timeout: timeoutMs },
  );
}

/** Wait until candles are flowing (probe_widget_candle_count > 0). */
export async function waitForCandles(page: Page, timeoutMs = 30_000): Promise<void> {
  await page.waitForFunction(
    () => {
      const ex = (window as any).__mr_wasm_exports;
      return ex && typeof ex.probe_widget_candle_count === 'function' && ex.probe_widget_candle_count() > 0;
    },
    { timeout: timeoutMs },
  );
}

/** Wait until the hello handshake completes (probe_md_hello_received == 1). */
export async function waitForHello(page: Page, timeoutMs = 20_000): Promise<void> {
  await page.waitForFunction(
    () => {
      const ex = (window as any).__mr_wasm_exports;
      return ex && typeof ex.probe_md_hello_received === 'function' && ex.probe_md_hello_received() === 1;
    },
    { timeout: timeoutMs },
  );
}

/**
 * Full boot sequence: canvas → WASM → hello → subscribe ACK → candles.
 * This replaces the old `waitForCanvas + waitForTimeout(8000)` pattern.
 */
export async function waitForFullBoot(page: Page, timeoutMs = 45_000): Promise<void> {
  // 1. Canvas element exists with context
  await page.waitForFunction(
    () => {
      const c = document.querySelector('canvas');
      return c && !!(c.getContext('2d') || c.getContext('webgl2'));
    },
    { timeout: timeoutMs },
  );

  // 2. WASM exports installed
  await page.waitForFunction(
    () => typeof (window as any).__mr_wasm_exports !== 'undefined',
    { timeout: timeoutMs },
  );

  // 3. Hello handshake
  await waitForHello(page, timeoutMs);

  // 4. At least one subscribe ACK
  await waitForSubscribeAck(page, timeoutMs);
}

/** Wait until trades are flowing (probe_widget_trades_count > 0). */
export async function waitForTrades(page: Page, timeoutMs = 30_000): Promise<void> {
  await page.waitForFunction(
    () => {
      const ex = (window as any).__mr_wasm_exports;
      return ex && typeof ex.probe_widget_trades_count === 'function' && ex.probe_widget_trades_count() > 0;
    },
    { timeout: timeoutMs },
  );
}

/** Wait until stats entries exist (probe_widget_stats_count > 0). */
export async function waitForStats(page: Page, timeoutMs = 30_000): Promise<void> {
  await page.waitForFunction(
    () => {
      const ex = (window as any).__mr_wasm_exports;
      return ex && typeof ex.probe_widget_stats_count === 'function' && ex.probe_widget_stats_count() > 0;
    },
    { timeout: timeoutMs },
  );
}

/** Wait until orderbook has depth (asks + bids > 0). */
export async function waitForOrderbook(page: Page, timeoutMs = 30_000): Promise<void> {
  await page.waitForFunction(
    () => {
      const ex = (window as any).__mr_wasm_exports;
      if (!ex || typeof ex.probe_widget_orderbook_asks !== 'function') return false;
      return ex.probe_widget_orderbook_asks() > 0 && ex.probe_widget_orderbook_bids() > 0;
    },
    { timeout: timeoutMs },
  );
}

/**
 * Wait for a specific localStorage key to have a value.
 * Useful for verifying persistence writes.
 */
export async function waitForLocalStorage(
  page: Page,
  key: string,
  timeoutMs = 10_000,
): Promise<string> {
  return page.waitForFunction(
    (k) => {
      const val = localStorage.getItem(k);
      return val !== null ? val : null;
    },
    key,
    { timeout: timeoutMs },
  ).then((handle) => handle.jsonValue() as Promise<string>);
}

/**
 * Wait for a probe to reach or exceed a target value.
 */
export async function waitForProbeValue(
  page: Page,
  probeName: string,
  minValue: number,
  timeoutMs = 20_000,
): Promise<void> {
  await page.waitForFunction(
    ([name, min]) => {
      const ex = (window as any).__mr_wasm_exports;
      if (!ex || typeof ex[name] !== 'function') return false;
      return ex[name]() >= min;
    },
    [probeName, minValue] as const,
    { timeout: timeoutMs },
  );
}
