/**
 * CanvasDriver — helpers for interacting with and inspecting the <canvas>.
 *
 * Since Market Raccoon renders entirely to Canvas2D (no DOM widgets),
 * visual assertions rely on pixel sampling + WASM probes rather than
 * DOM selectors.
 */

import type { Page } from '@playwright/test';

export interface PixelRGBA {
  r: number;
  g: number;
  b: number;
  a: number;
}

export class CanvasDriver {
  constructor(private readonly page: Page) {}

  /** Wait until <canvas> exists and has a 2D/WebGL context. */
  async waitForCanvas(timeoutMs = 15_000): Promise<void> {
    await this.page.waitForFunction(
      () => {
        const c = document.querySelector('canvas');
        if (!c) return false;
        return !!(c.getContext('2d') || c.getContext('webgl2') || c.getContext('webgl'));
      },
      { timeout: timeoutMs },
    );
  }

  /** Get the canvas dimensions as reported by the element. */
  async dimensions(): Promise<{ width: number; height: number }> {
    return this.page.evaluate(() => {
      const c = document.querySelector('canvas')!;
      return { width: c.width, height: c.height };
    });
  }

  /** Sample a single pixel at (x, y) from the canvas. */
  async samplePixel(x: number, y: number): Promise<PixelRGBA> {
    return this.page.evaluate(
      ([px, py]) => {
        const c = document.querySelector('canvas')!;
        const ctx = c.getContext('2d')!;
        const d = ctx.getImageData(px, py, 1, 1).data;
        return { r: d[0], g: d[1], b: d[2], a: d[3] };
      },
      [x, y] as const,
    );
  }

  /**
   * Capture a rectangular region of pixels as a flat Uint8 array (RGBA).
   * Returns { width, height, data } where data is number[].
   */
  async captureRegion(
    x: number,
    y: number,
    w: number,
    h: number,
  ): Promise<{ width: number; height: number; data: number[] }> {
    return this.page.evaluate(
      ([rx, ry, rw, rh]) => {
        const c = document.querySelector('canvas')!;
        const ctx = c.getContext('2d')!;
        const img = ctx.getImageData(rx, ry, rw, rh);
        return { width: img.width, height: img.height, data: Array.from(img.data) };
      },
      [x, y, w, h] as const,
    );
  }

  /** Check whether the canvas has any non-black, non-transparent pixels in the center region. */
  async hasVisibleContent(): Promise<boolean> {
    return this.page.evaluate(() => {
      const c = document.querySelector('canvas')!;
      const ctx = c.getContext('2d')!;
      const cx = Math.floor(c.width / 2);
      const cy = Math.floor(c.height / 2);
      const size = 100;
      const img = ctx.getImageData(cx - size / 2, cy - size / 2, size, size);
      for (let i = 0; i < img.data.length; i += 4) {
        if (img.data[i] > 10 || img.data[i + 1] > 10 || img.data[i + 2] > 10) {
          if (img.data[i + 3] > 0) return true;
        }
      }
      return false;
    });
  }
}
