// storage.js — localStorage settings bridge, clipboard write, synchronous HTTP GET.
//
// These are odin_env foreign procs that interact with browser APIs.

import { loadStringRaw, encodeString } from "./memory.js";

const SETTINGS_STORAGE_PREFIX = "mr.settings.";

function settingsKey(key) {
    return `${SETTINGS_STORAGE_PREFIX}${key}`;
}

export function createStorageProcs() {
    return {
        web_settings_load(key_ptr, key_len, out_ptr, out_cap, buffer) {
            if (!buffer || out_cap <= 0) return 0;
            const key = loadStringRaw(buffer, key_ptr, key_len);
            if (!key) return 0;
            try {
                const value = window.localStorage.getItem(settingsKey(key));
                if (typeof value !== "string" || value.length === 0) return 0;
                const encoded = encodeString(value);
                const copyLen = Math.min(encoded.length, out_cap);
                new Uint8Array(buffer, out_ptr, copyLen).set(encoded.subarray(0, copyLen));
                return copyLen;
            } catch (_) {
                return 0;
            }
        },

        web_settings_save(key_ptr, key_len, value_ptr, value_len, buffer) {
            if (!buffer) return 0;
            const key = loadStringRaw(buffer, key_ptr, key_len);
            if (!key) return 0;
            const value = value_len > 0 ? loadStringRaw(buffer, value_ptr, value_len) : "";
            try {
                window.localStorage.setItem(settingsKey(key), value);
                return 1;
            } catch (_) {
                return 0;
            }
        },

        web_clipboard_write(text_ptr, text_len, buffer) {
            if (!buffer || text_len <= 0) return 0;
            const text = loadStringRaw(buffer, text_ptr, text_len);
            if (!text) return 0;
            try {
                navigator.clipboard.writeText(text).catch(() => {});
                return 1;
            } catch (_) {
                return 0;
            }
        },

        http_get_sync(url_ptr, url_len, out_ptr, out_cap, buffer) {
            if (!buffer || out_cap <= 0) return 0;
            const url = loadStringRaw(buffer, url_ptr, url_len);
            if (!url) return 0;
            try {
                const xhr = new XMLHttpRequest();
                xhr.open("GET", url, false);
                xhr.setRequestHeader("Accept", "application/json");
                xhr.send(null);
                if (xhr.status !== 200) {
                    // S111: Non-200 is a normal condition (backend not ready, no data).
                    // Return 0 without logging — caller handles gracefully.
                    return 0;
                }
                const body = xhr.responseText || "";
                if (!body) return 0;
                const encoded = encodeString(body);
                const copyLen = Math.min(encoded.length, out_cap);
                new Uint8Array(buffer, out_ptr, copyLen).set(encoded.subarray(0, copyLen));
                return copyLen;
            } catch (_) {
                return 0;
            }
        },
    };
}
