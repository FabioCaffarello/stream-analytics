// websocket.js — WebSocket bridge: lifecycle, message queue, auth, runtime override.
//
// Message queue is drained by WASM poll each frame. Auth supports API key
// and JWT token via query params (browser WebSocket has no custom headers).

import { loadStringRaw, encodeString } from "./memory.js";

const WS_MSG_QUEUE_CAP = 4096;

function defaultWsUrlForCurrentOrigin() {
    const proto = window.location.protocol === "https:" ? "wss" : "ws";
    const host = window.location.host || "127.0.0.1:8090";
    return `${proto}://${host}/ws`;
}

export function initWebSocket() {
    let ws = null;
    let wsState = 0; // 0=closed, 1=connecting, 2=open, 3=closing
    const msgQueue = [];
    let lastMsgTs = 0;
    let dropCount = 0;
    let epoch = 0;
    const runtimeOverride = { ws_url: "", api_key: "" };

    // --- auth helpers ---

    function parseAuthFromHeaderString(hdrs) {
        if (!hdrs || typeof hdrs !== "string") return { api_key: "", jwt_token: "" };
        const jwtMatch = hdrs.match(/Authorization:\s*Bearer\s+(\S+)/i);
        if (jwtMatch && typeof jwtMatch[1] === "string") {
            return { api_key: "", jwt_token: jwtMatch[1].trim() };
        }
        const apiMatch = hdrs.match(/X-API-Key:\s*(\S+)/i);
        return {
            api_key: apiMatch && typeof apiMatch[1] === "string" ? apiMatch[1].trim() : "",
            jwt_token: "",
        };
    }

    function buildWsUrlWithAuth(baseUrl, apiKey, jwtToken) {
        if (!baseUrl) return "";
        if (jwtToken) {
            const sep = baseUrl.includes("?") ? "&" : "?";
            return `${baseUrl}${sep}token=${encodeURIComponent(jwtToken)}`;
        }
        if (!apiKey) return baseUrl;
        const sep = baseUrl.includes("?") ? "&" : "?";
        return `${baseUrl}${sep}api_key=${encodeURIComponent(apiKey)}`;
    }

    function sanitizeWsUrlForLog(wsUrl) {
        if (!wsUrl || typeof wsUrl !== "string") return wsUrl || "";
        try {
            const u = new URL(wsUrl, window.location.href);
            if (u.searchParams.has("api_key")) u.searchParams.set("api_key", "***");
            if (u.searchParams.has("token")) u.searchParams.set("token", "***");
            if (u.searchParams.has("jwt")) u.searchParams.set("jwt", "***");
            return u.toString();
        } catch (_) {
            return wsUrl
                .replace(/([?&]api_key=)[^&]*/gi, "$1***")
                .replace(/([?&]token=)[^&]*/gi, "$1***")
                .replace(/([?&]jwt=)[^&]*/gi, "$1***");
        }
    }

    function resolveWsConfig(url, hdrs) {
        const baseUrl = (runtimeOverride.ws_url || defaultWsUrlForCurrentOrigin() || url || "").trim();
        const parsed = parseAuthFromHeaderString(hdrs);
        const apiKey = (runtimeOverride.api_key || parsed.api_key).trim();
        const jwtToken = parsed.jwt_token;
        return {
            base_url: baseUrl,
            api_key: apiKey,
            jwt_token: jwtToken,
            ws_url: buildWsUrlWithAuth(baseUrl, apiKey, jwtToken),
            override_active: runtimeOverride.ws_url.length > 0 || runtimeOverride.api_key.length > 0,
        };
    }

    // --- socket lifecycle ---

    function closeActiveSocket(markClosing = false) {
        if (!ws) return;
        if (markClosing) wsState = 3;
        try {
            ws.onopen = ws.onmessage = ws.onclose = ws.onerror = null;
            ws.close();
        } catch (_) {}
        ws = null;
    }

    function connectSocketUrl(wsUrl) {
        if (!wsUrl || typeof wsUrl !== "string") {
            wsState = 0;
            console.error("[ws] invalid url");
            return false;
        }

        closeActiveSocket(false);
        wsState = 1;
        msgQueue.length = 0;

        try {
            ws = new WebSocket(wsUrl);
        } catch (e) {
            console.error("[ws] connect error:", e);
            wsState = 0;
            return false;
        }

        const wsLocal = ws;
        const localEpoch = ++epoch;
        ws.onopen = () => {
            if (ws !== wsLocal || epoch !== localEpoch) return;
            wsState = 2;
            console.log("[ws] connected to", sanitizeWsUrlForLog(wsUrl));
        };
        ws.onmessage = (ev) => {
            if (ws !== wsLocal || epoch !== localEpoch) return;
            if (typeof ev.data === "string") {
                if (msgQueue.length >= WS_MSG_QUEUE_CAP) {
                    msgQueue.shift();
                    dropCount += 1;
                }
                msgQueue.push(ev.data);
                lastMsgTs = performance.now();
            }
        };
        ws.onclose = (ev) => {
            if (ws !== wsLocal || epoch !== localEpoch) return;
            wsState = 0;
            console.log("[ws] closed code=" + ev.code);
        };
        ws.onerror = () => {
            if (ws !== wsLocal || epoch !== localEpoch) return;
            wsState = 0;
            console.error("[ws] error");
        };

        return true;
    }

    // --- runtime config API ---

    function configSnapshot() {
        return {
            mode: runtimeOverride.ws_url || runtimeOverride.api_key ? "runtime-override" : "default",
            ws_url: runtimeOverride.ws_url,
            api_key: runtimeOverride.api_key,
            default_ws_url: defaultWsUrlForCurrentOrigin(),
        };
    }

    function switchRuntime(wsUrl, apiKey, options = {}) {
        if (typeof wsUrl === "string") runtimeOverride.ws_url = wsUrl.trim();
        if (typeof apiKey === "string") runtimeOverride.api_key = apiKey.trim();
        const live = !options || options.live !== false;
        if (!live) return configSnapshot();

        closeActiveSocket(false);
        wsState = 0;
        msgQueue.length = 0;
        lastMsgTs = 0;

        return configSnapshot();
    }

    // --- window.__mr_* APIs ---

    window.__mr_get_runtime_config = () => configSnapshot();
    window.__mr_set_runtime_config = (config = {}) => switchRuntime(config.ws_url, config.api_key, { live: true });
    window.__mr_set_runtime_config_live = (config = {}) => switchRuntime(config.ws_url, config.api_key, { live: true });
    window.__mr_set_ws_endpoint = (wsUrl, apiKey, options = {}) => switchRuntime(wsUrl, apiKey, options);
    window.__mr_switch_ws_runtime = switchRuntime;
    window.__mr_clear_ws_runtime_override = (options = {}) => {
        runtimeOverride.ws_url = "";
        runtimeOverride.api_key = "";
        const live = !options || options.live !== false;
        if (live) {
            closeActiveSocket(false);
            wsState = 0;
            msgQueue.length = 0;
            lastMsgTs = 0;
        }
        return configSnapshot();
    };

    // --- odin_env procs ---

    return {
        // State accessors for idle throttle / perf HUD
        getState: () => wsState,
        getQueueLength: () => msgQueue.length,
        getDropCount: () => dropCount,
        getLastMsgTs: () => lastMsgTs,
        configSnapshot,

        // odin_env foreign procs
        ws_connect(url_ptr, url_len, hdr_ptr, hdr_len, buffer) {
            const url = loadStringRaw(buffer, url_ptr, url_len);
            const hdrs = hdr_len > 0 ? loadStringRaw(buffer, hdr_ptr, hdr_len) : "";
            const resolved = resolveWsConfig(url, hdrs);
            connectSocketUrl(resolved.ws_url);
        },

        ws_send(ptr, len, buffer) {
            if (!ws || ws.readyState !== WebSocket.OPEN) return;
            const msg = loadStringRaw(buffer, ptr, len);
            ws.send(msg);
        },

        ws_close() {
            closeActiveSocket(true);
        },

        ws_state: () => wsState,

        ws_drop_count: () => dropCount >>> 0,

        ws_poll_msg(buf_ptr, buf_len, buffer) {
            if (msgQueue.length === 0) return 0;
            const msg = msgQueue.shift();
            const encoded = encodeString(msg);
            const copyLen = Math.min(encoded.length, buf_len);
            new Uint8Array(buffer, buf_ptr, copyLen).set(encoded.subarray(0, copyLen));
            if (encoded.length > buf_len) return -copyLen;
            return copyLen;
        },
    };
}
