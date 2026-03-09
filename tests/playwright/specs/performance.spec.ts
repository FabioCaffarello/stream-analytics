/**
 * Performance Probes — validates render budgets and latency thresholds.
 *
 * These tests verify that the client operates within its performance
 * contracts after sustained data flow. They catch regressions in the
 * parse → apply → render pipeline.
 */

import { test, expect } from '../fixtures/mr-test';

/** Maximum acceptable p95 render time in microseconds (2ms). */
const RENDER_P95_BUDGET_US = 2_000;

/** Maximum acceptable p99 render time in microseconds (5ms). */
const RENDER_P99_BUDGET_US = 5_000;

/** Maximum acceptable parse p95 in microseconds (500us). */
const PARSE_P95_BUDGET_US = 500;

/** Maximum acceptable parse p99 in microseconds (1ms). */
const PARSE_P99_BUDGET_US = 1_000;

test.describe('Performance Probes', () => {
  test.beforeEach(async ({ dash }) => {
    await dash.bootWithData();
    // Let data flow for 10s to build meaningful percentiles
    await dash.page.waitForTimeout(10_000);
  });

  // ── Render budgets ─────────────────────────────────────────────────

  test('stats widget render p95 within budget', async ({ probe }) => {
    const p95 = await probe.statsRenderP95Us();
    // p95 = 0 means not enough samples yet — skip assertion
    if (p95 > 0) {
      expect(p95).toBeLessThanOrEqual(RENDER_P95_BUDGET_US);
    }
  });

  test('stats widget zero over-budget renders', async ({ probe }) => {
    expect(await probe.statsRenderOverBudget()).toBe(0);
  });

  test('tape widget render p95 within budget', async ({ probe }) => {
    const p95 = await probe.tapeRenderP95Us();
    if (p95 > 0) {
      expect(p95).toBeLessThanOrEqual(RENDER_P95_BUDGET_US);
    }
  });

  test('tape widget zero over-budget renders', async ({ probe }) => {
    expect(await probe.tapeRenderOverBudget()).toBe(0);
  });

  test('DOM widget render p95 within budget', async ({ probe }) => {
    const p95 = await probe.domRenderP95Us();
    if (p95 > 0) {
      expect(p95).toBeLessThanOrEqual(RENDER_P95_BUDGET_US);
    }
  });

  test('DOM widget zero over-budget renders', async ({ probe }) => {
    expect(await probe.domRenderOverBudget()).toBe(0);
  });

  // ── Parse/apply latency ────────────────────────────────────────────

  test('message parse p95 within budget', async ({ probe }) => {
    const p95 = await probe.mdParseP95Us();
    if (p95 > 0) {
      expect(p95).toBeLessThanOrEqual(PARSE_P95_BUDGET_US);
    }
  });

  test('message parse p99 within budget', async ({ probe }) => {
    const p99 = await probe.mdParseP99Us();
    if (p99 > 0) {
      expect(p99).toBeLessThanOrEqual(PARSE_P99_BUDGET_US);
    }
  });

  test('message apply p95 within budget', async ({ probe }) => {
    const p95 = await probe.mdApplyP95Us();
    if (p95 > 0) {
      expect(p95).toBeLessThanOrEqual(PARSE_P95_BUDGET_US);
    }
  });

  // ── Backlog probes (should not accumulate under normal load) ───────

  test('trade backlog stays near zero', async ({ probe }) => {
    const backlog = await probe.tradeBacklog();
    // Under normal operation, backlog should be small (< 100)
    expect(backlog).toBeLessThan(100);
  });

  test('candle backlog stays near zero', async ({ probe }) => {
    const backlog = await probe.candleBacklog();
    expect(backlog).toBeLessThan(100);
  });

  // ── Transport mode ─────────────────────────────────────────────────

  test('transport mode is Terminal_V1', async ({ probe }) => {
    const mode = await probe.mdTransportMode();
    // 0 = Terminal_V1 (preferred), 1 = Legacy_JSON
    expect(mode).toBe(0);
  });

  // ── No widget drops ────────────────────────────────────────────────

  test('zero widget drops after 10s steady state', async ({ probe }) => {
    expect(await probe.statsDropTotal()).toBe(0);
    expect(await probe.tapeDropTotal()).toBe(0);
    expect(await probe.domDropTotal()).toBe(0);
  });
});
