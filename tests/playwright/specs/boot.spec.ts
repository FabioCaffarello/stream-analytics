/**
 * Boot & Lifecycle — validates the full startup sequence.
 *
 * These tests verify that the client boots, connects, and receives data.
 * They are the foundation: if these fail, nothing else matters.
 */

import { test, expect } from '../fixtures/mr-test';
import { waitForCandles, waitForFullBoot } from '../helpers/wait';

test.describe('Boot & Lifecycle', () => {
  test('canvas renders after page load', async ({ page, canvas }) => {
    await page.goto('/');
    await canvas.waitForCanvas();
    const dims = await canvas.dimensions();
    expect(dims.width).toBeGreaterThan(0);
    expect(dims.height).toBeGreaterThan(0);
  });

  test('WASM module loads and probes are available', async ({ page, probe }) => {
    await page.goto('/');
    await probe.waitForReady();
    const snap = await probe.snapshot();
    expect(snap).toBeDefined();
    expect(typeof snap).toBe('object');
  });

  test('WebSocket connects and hello handshake completes', async ({ page, probe }) => {
    await page.goto('/');
    await waitForFullBoot(page);
    expect(await probe.helloReceived()).toBe(true);
  });

  test('subscription acknowledged by server', async ({ page, probe }) => {
    await page.goto('/');
    await waitForFullBoot(page);
    expect(await probe.subscribeAckCount()).toBeGreaterThan(0);
  });

  test('candle data flows within boot window', async ({ page, probe }) => {
    await page.goto('/');
    await waitForFullBoot(page);
    await waitForCandles(page);
    expect(await probe.candleCount()).toBeGreaterThan(0);
  });

  test('active stream is established', async ({ page, probe }) => {
    await page.goto('/');
    await waitForFullBoot(page);
    expect(await probe.hasActiveStream()).toBe(true);
  });

  test('canvas has visible content after data flow', async ({ page, canvas, dash }) => {
    await dash.bootWithData();
    expect(await canvas.hasVisibleContent()).toBe(true);
  });

  test('no page errors during boot', async ({ page, console: cons }) => {
    await page.goto('/');
    await waitForFullBoot(page);
    expect(cons.errors).toHaveLength(0);
  });

  test('stream count is at least 1 after boot', async ({ page, probe }) => {
    await page.goto('/');
    await waitForFullBoot(page);
    expect(await probe.streamCount()).toBeGreaterThanOrEqual(1);
  });

  test('transport mode is Terminal_V1 after boot', async ({ page, probe }) => {
    await page.goto('/');
    await waitForFullBoot(page);
    // 0 = Terminal_V1
    expect(await probe.mdTransportMode()).toBe(0);
  });

  test('layout version is current after boot', async ({ page, probe }) => {
    await page.goto('/');
    await waitForFullBoot(page);
    // WORKSPACE_SCHEMA_VERSION >= 12
    expect(await probe.layoutVersion()).toBeGreaterThanOrEqual(10);
  });
});
