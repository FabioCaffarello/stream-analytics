/**
 * UI Modes — validates toggleable UI elements (zen, indicators, help, detail, picker).
 *
 * Since everything renders to canvas, we verify mode activation through probes
 * and screenshot diffing where applicable.
 */

import { test, expect } from '../fixtures/mr-test';

test.describe('UI Modes', () => {
  test.beforeEach(async ({ dash }) => {
    await dash.bootWithData();
  });

  test('zen mode toggles without errors', async ({ dash, console: cons }) => {
    await dash.toggleZenMode();
    await dash.screenshot('zen-mode-on');
    await dash.toggleZenMode();
    await dash.screenshot('zen-mode-off');
    expect(cons.errors).toHaveLength(0);
  });

  test('indicators panel toggles without errors', async ({ dash, console: cons }) => {
    await dash.toggleIndicators();
    await dash.screenshot('indicators-on');
    await dash.toggleIndicators();
    expect(cons.errors).toHaveLength(0);
  });

  test('help overlay toggles without errors', async ({ dash, console: cons }) => {
    await dash.toggleHelp();
    await dash.screenshot('help-on');
    await dash.toggleHelp();
    expect(cons.errors).toHaveLength(0);
  });

  test('detail panel toggles without errors', async ({ dash, console: cons }) => {
    await dash.toggleDetailPanel();
    await dash.screenshot('detail-on');
    await dash.toggleDetailPanel();
    expect(cons.errors).toHaveLength(0);
  });

  test('stream picker opens and dismisses', async ({ dash, console: cons }) => {
    await dash.openStreamPicker();
    await dash.screenshot('picker-open');
    await dash.pickerDismiss();
    expect(cons.errors).toHaveLength(0);
  });

  test('stream picker can select next stream', async ({ dash, probe }) => {
    const switchesBefore = await probe.streamSwitchesTotal();
    await dash.openStreamPicker();
    await dash.pickerSelectNext();
    await dash.pickerConfirm();
    // Wait for the stream to switch and data to arrive
    await dash.page.waitForTimeout(5_000);
    const switchesAfter = await probe.streamSwitchesTotal();
    expect(switchesAfter).toBeGreaterThan(switchesBefore);
  });
});
