/**
 * ConsoleCollector — captures browser console messages and page errors.
 *
 * Attach early (before navigation) so no messages are missed.
 */

import type { Page } from '@playwright/test';

export interface ConsoleEntry {
  type: string;
  text: string;
  ts: number;
}

export class ConsoleCollector {
  readonly messages: ConsoleEntry[] = [];
  readonly errors: string[] = [];

  constructor(private readonly page: Page) {
    page.on('console', (msg) => {
      this.messages.push({
        type: msg.type(),
        text: msg.text(),
        ts: Date.now(),
      });
    });
    page.on('pageerror', (err) => {
      this.errors.push(String(err));
    });
  }

  /** Filter messages containing a substring (case-insensitive). */
  filter(substring: string): ConsoleEntry[] {
    const lower = substring.toLowerCase();
    return this.messages.filter((m) => m.text.toLowerCase().includes(lower));
  }

  /** True if any page error was captured. */
  hasErrors(): boolean {
    return this.errors.length > 0;
  }

  /** True if console contains the given substring. */
  has(substring: string): boolean {
    return this.filter(substring).length > 0;
  }

  /** Return all error-level console messages. */
  consoleErrors(): ConsoleEntry[] {
    return this.messages.filter((m) => m.type === 'error');
  }
}
