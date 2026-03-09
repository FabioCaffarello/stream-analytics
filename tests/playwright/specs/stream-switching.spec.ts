/**
 * Stream Switching — validates changing the active market stream.
 *
 * When the user picks a different instrument via the stream picker,
 * the client unsubscribes from the old stream and subscribes to the new one.
 * We verify the subscription lifecycle and data arrival.
 */

import { test, expect } from '../fixtures/mr-test';
import { waitForCandles, waitForSubscribeAck } from '../helpers/wait';

test.describe('Stream Switching', () => {
  test.beforeEach(async ({ dash }) => {
    await dash.bootWithData();
  });

  test('stream switch via picker changes active subject', async ({ dash, probe }) => {
    const subjectBefore = await probe.activeSubjectLo32();

    await dash.openStreamPicker();
    await dash.pickerSelectNext();
    await dash.pickerConfirm();

    // Wait for new subscription to be acknowledged
    await dash.page.waitForTimeout(8_000);
    const subjectAfter = await probe.activeSubjectLo32();

    // Subject should have changed (different instrument)
    expect(subjectAfter).not.toBe(subjectBefore);
  });

  test('candles flow after stream switch', async ({ dash, probe }) => {
    await dash.openStreamPicker();
    await dash.pickerSelectNext();
    await dash.pickerConfirm();

    await waitForCandles(dash.page);
    expect(await probe.candleCount()).toBeGreaterThan(0);
  });

  test('stream switch counter increments', async ({ dash, probe }) => {
    const switchesBefore = await probe.streamSwitchesTotal();

    await dash.openStreamPicker();
    await dash.pickerSelectNext();
    await dash.pickerConfirm();
    await dash.page.waitForTimeout(5_000);

    const switchesAfter = await probe.streamSwitchesTotal();
    expect(switchesAfter).toBeGreaterThan(switchesBefore);
  });

  test('subscribe ACK received for new stream', async ({ dash, probe }) => {
    await dash.openStreamPicker();
    await dash.pickerSelectNext();
    await dash.pickerConfirm();

    await waitForSubscribeAck(dash.page);
    expect(await probe.subscribeAckCount()).toBeGreaterThan(0);
  });

  test('no seq gaps after stream switch', async ({ dash, probe }) => {
    await dash.openStreamPicker();
    await dash.pickerSelectNext();
    await dash.pickerConfirm();

    await dash.page.waitForTimeout(10_000);
    expect(await probe.seqGapCount()).toBe(0);
  });

  test('stream switch + TF switch combined', async ({ dash, probe }) => {
    // Switch stream
    await dash.openStreamPicker();
    await dash.pickerSelectNext();
    await dash.pickerConfirm();
    await waitForCandles(dash.page);

    // Then switch TF
    await dash.switchTimeframe('15m');
    await dash.page.waitForTimeout(3_000);

    expect(await probe.candleCount()).toBeGreaterThan(0);
    expect(await probe.seqGapCount()).toBe(0);
  });

  test('active stream persists to localStorage', async ({ dash }) => {
    await dash.openStreamPicker();
    await dash.pickerSelectNext();
    await dash.pickerConfirm();
    await dash.page.waitForTimeout(5_000);

    const venue = await dash.getLocalStorage('mr.settings.active_stream_venue');
    const symbol = await dash.getLocalStorage('mr.settings.active_stream_symbol');
    // Both should be non-null after a stream switch
    expect(venue).not.toBeNull();
    expect(symbol).not.toBeNull();
  });
});
