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

        // S126: Synchronous HTTP PUT — used for workspace persistence to backend.
        // Returns bytes written to out_ptr (response body), or 0 on failure.
        http_put_sync(url_ptr, url_len, body_ptr, body_len, out_ptr, out_cap, buffer) {
            if (!buffer) return 0;
            const url = loadStringRaw(buffer, url_ptr, url_len);
            if (!url) return 0;
            const body = body_len > 0 ? loadStringRaw(buffer, body_ptr, body_len) : "";
            try {
                const xhr = new XMLHttpRequest();
                xhr.open("PUT", url, false);
                xhr.setRequestHeader("Content-Type", "application/json");
                xhr.setRequestHeader("Accept", "application/json");
                xhr.send(body);
                if (xhr.status !== 200) return 0;
                const resp = xhr.responseText || "";
                if (!resp || out_cap <= 0) return 0;
                const encoded = encodeString(resp);
                const copyLen = Math.min(encoded.length, out_cap);
                new Uint8Array(buffer, out_ptr, copyLen).set(encoded.subarray(0, copyLen));
                return copyLen;
            } catch (_) {
                return 0;
            }
        },

        // S126: Load workspace state from backend into localStorage.
        // GET /api/v1/workspace → if 200, write each setting to localStorage.
        // Returns 1 if workspace was loaded and applied, 0 otherwise.
        web_workspace_load(buffer) {
            try {
                const xhr = new XMLHttpRequest();
                xhr.open("GET", "/api/v1/workspace", false);
                xhr.setRequestHeader("Accept", "application/json");
                xhr.send(null);
                if (xhr.status === 204) return 0; // First run — no saved state.
                if (xhr.status !== 200) return 0;
                const data = JSON.parse(xhr.responseText);
                if (!data || !data.layout_v6) return 0;
                // Write V6 layout to localStorage.
                window.localStorage.setItem(settingsKey("layout_v6"), data.layout_v6);
                if (data.schema_version) {
                    window.localStorage.setItem(settingsKey("settings_version"), String(data.schema_version));
                }
                // Write all settings keys to localStorage.
                if (data.settings && typeof data.settings === "object") {
                    for (const [k, v] of Object.entries(data.settings)) {
                        window.localStorage.setItem(settingsKey(k), String(v));
                    }
                }
                return 1;
            } catch (_) {
                return 0;
            }
        },

        // S126: Sync current localStorage workspace state to backend.
        // Collects all mr.settings.* keys, builds JSON payload, PUTs to /api/v1/workspace.
        // Returns 1 on success, 0 on failure.
        web_workspace_sync(buffer) {
            try {
                const layoutV6 = window.localStorage.getItem(settingsKey("layout_v6"));
                if (!layoutV6) return 0; // Nothing to sync.
                const versionStr = window.localStorage.getItem(settingsKey("settings_version"));
                const schemaVersion = versionStr ? parseInt(versionStr, 10) : 0;
                if (schemaVersion <= 0) return 0;
                // Collect all settings (exclude layout_v6 and settings_version — they're top-level).
                const settings = {};
                for (let i = 0; i < window.localStorage.length; i++) {
                    const key = window.localStorage.key(i);
                    if (!key || !key.startsWith(SETTINGS_STORAGE_PREFIX)) continue;
                    const shortKey = key.slice(SETTINGS_STORAGE_PREFIX.length);
                    if (shortKey === "layout_v6" || shortKey === "settings_version") continue;
                    settings[shortKey] = window.localStorage.getItem(key) || "";
                }
                const payload = JSON.stringify({
                    schema_version: schemaVersion,
                    layout_v6: layoutV6,
                    settings: settings,
                });
                const xhr = new XMLHttpRequest();
                xhr.open("PUT", "/api/v1/workspace", false);
                xhr.setRequestHeader("Content-Type", "application/json");
                xhr.setRequestHeader("Accept", "application/json");
                xhr.send(payload);
                return xhr.status === 200 ? 1 : 0;
            } catch (_) {
                return 0;
            }
        },
    };
}
