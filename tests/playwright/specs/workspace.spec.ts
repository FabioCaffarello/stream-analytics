/**
 * Workspace Persistence — validates save/load/reload of layout and settings.
 *
 * The client persists state to localStorage (mr.settings.*) and syncs
 * to the backend via PUT /api/v1/workspace. These tests verify that
 * user state survives page reloads and that the workspace schema is
 * consistent.
 */

import { test, expect } from '../fixtures/mr-test';
import { waitForFullBoot, waitForCandles } from '../helpers/wait';

test.describe('Workspace Persistence', () => {
  test('settings are written to localStorage after boot', async ({ dash }) => {
    await dash.bootWithData();
    const settings = await dash.getWorkspaceSettings();
    // At minimum, the active TF and layout should be persisted
    const keys = Object.keys(settings);
    expect(keys.length).toBeGreaterThan(0);
  });

  test('active timeframe persists to localStorage', async ({ dash }) => {
    await dash.bootWithData();
    await dash.switchTimeframe('15m');
    // Allow persistence cycle
    await dash.page.waitForTimeout(2_000);
    const tfVal = await dash.getLocalStorage('mr.settings.active_tf_idx');
    expect(tfVal).not.toBeNull();
    // TF index 3 = 15m
    expect(tfVal).toBe('3');
  });

  test('timeframe survives page reload', async ({ dash, probe }) => {
    await dash.bootWithData();
    await dash.switchTimeframe('1h');
    await dash.page.waitForTimeout(2_000);

    // Reload page
    await dash.page.reload();
    await waitForFullBoot(dash.page);
    await waitForCandles(dash.page);

    // TF should be restored to 1h (index 4)
    const tfIdx = await probe.activeTfIndex();
    expect(tfIdx).toBe(4);
  });

  test('layout_v6 is written to localStorage', async ({ dash }) => {
    await dash.bootWithData();
    await dash.page.waitForTimeout(3_000);
    const layout = await dash.getLocalStorage('mr.settings.layout_v6');
    // Layout should exist and have non-trivial content
    expect(layout).not.toBeNull();
    expect(layout!.length).toBeGreaterThan(10);
  });

  test('schema version is set in localStorage', async ({ dash }) => {
    await dash.bootWithData();
    await dash.page.waitForTimeout(2_000);
    const version = await dash.getLocalStorage('mr.settings.settings_version');
    expect(version).not.toBeNull();
    const ver = parseInt(version!, 10);
    // Current schema version should be >= 10 (we're at V12)
    expect(ver).toBeGreaterThanOrEqual(10);
  });

  test('workspace layout_v6 has CRC integrity footer', async ({ dash }) => {
    await dash.bootWithData();
    await dash.page.waitForTimeout(3_000);
    const layout = await dash.getLocalStorage('mr.settings.layout_v6');
    expect(layout).not.toBeNull();
    // S122: layout string ends with |CK:XXXXXXXX (FNV-1a checksum)
    expect(layout).toMatch(/\|CK:[0-9A-Fa-f]{8}$/);
  });

  test('clean localStorage produces valid default workspace', async ({ dash, probe }) => {
    // Clear everything before boot
    await dash.page.goto('/');
    await dash.clearLocalStorage();
    await dash.page.reload();
    await waitForFullBoot(dash.page);
    await waitForCandles(dash.page);

    // Should have a valid active stream and candles
    expect(await probe.hasActiveStream()).toBe(true);
    expect(await probe.candleCount()).toBeGreaterThan(0);
  });

  test('settings accumulate correctly across TF switches', async ({ dash }) => {
    await dash.bootWithData();
    const settingsBefore = await dash.getWorkspaceSettings();
    const keysBefore = Object.keys(settingsBefore).length;

    await dash.switchTimeframe('5m');
    await dash.switchTimeframe('1h');
    await dash.page.waitForTimeout(2_000);

    const settingsAfter = await dash.getWorkspaceSettings();
    const keysAfter = Object.keys(settingsAfter).length;
    // Should not lose settings during switches
    expect(keysAfter).toBeGreaterThanOrEqual(keysBefore);
  });
});
