// canvas.js — Canvas2D host: sizing, DPR, font/fill/stroke caches, text measure.
//
// Exports an init function that wires ResizeObserver and returns the canvas API
// consumed by the WASM import object and the frame loop.

import { loadStringRaw } from "./memory.js";

const TEXT_MEASURE_CACHE_CAP = 2048;

export function initCanvas() {
    const canvas = document.getElementById("canvas");
    const ctx = canvas ? canvas.getContext("2d") : null;

    let sizeDirty = true;
    let dpr = window.devicePixelRatio || 1;

    // Cached Canvas2D state keys — avoid redundant API calls.
    let fontKey = "";
    let fillKey = "";
    let strokeKey = "";
    let lineW = -1;
    const measureCache = new Map();

    // --- internal helpers ---

    function setFont(size) {
        if (!ctx) return;
        const key = `${size}px monospace`;
        if (fontKey === key) return;
        fontKey = key;
        ctx.font = key;
    }

    function setFill(r, g, b, a) {
        if (!ctx) return;
        const key = `${(r * 255) | 0},${(g * 255) | 0},${(b * 255) | 0},${a}`;
        if (fillKey === key) return;
        fillKey = key;
        ctx.fillStyle = `rgba(${key})`;
    }

    function setStroke(r, g, b, a) {
        if (!ctx) return;
        const key = `${(r * 255) | 0},${(g * 255) | 0},${(b * 255) | 0},${a}`;
        if (strokeKey === key) return;
        strokeKey = key;
        ctx.strokeStyle = `rgba(${key})`;
    }

    function setLineWidth(thickness) {
        if (!ctx) return;
        if (lineW === thickness) return;
        lineW = thickness;
        ctx.lineWidth = thickness;
    }

    function markDirty() {
        sizeDirty = true;
    }

    function invalidateCaches() {
        fontKey = "";
        fillKey = "";
        strokeKey = "";
        lineW = -1;
        measureCache.clear();
    }

    // --- resize wiring ---

    if (canvas) {
        if (typeof ResizeObserver === "function") {
            new ResizeObserver(markDirty).observe(canvas);
        }
        window.addEventListener("resize", markDirty, { passive: true });
        if (window.visualViewport) {
            window.visualViewport.addEventListener("resize", markDirty, { passive: true });
        }
    }

    // --- public API ---

    function syncSize(force = false) {
        if (!canvas) return;
        const newDpr = window.devicePixelRatio || 1;
        if (newDpr !== dpr) {
            dpr = newDpr;
            sizeDirty = true;
        }
        if (!force && !sizeDirty) return;
        const rect = canvas.getBoundingClientRect();
        const w = Math.max(1, Math.round(rect.width * dpr));
        const h = Math.max(1, Math.round(rect.height * dpr));
        if (canvas.width !== w || canvas.height !== h) {
            canvas.width = w;
            canvas.height = h;
            if (ctx) ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
            invalidateCaches();
        }
        sizeDirty = false;
    }

    function isSizeDirty() {
        return sizeDirty;
    }

    // --- odin_env foreign procs ---

    function canvas_clear(r, g, b, a) {
        if (!ctx) return;
        ctx.save();
        ctx.setTransform(1, 0, 0, 1, 0, 0);
        setFill(r, g, b, a);
        ctx.fillRect(0, 0, canvas.width, canvas.height);
        ctx.restore();
    }

    function canvas_fill_rect(x, y, w, h, r, g, b, a) {
        if (!ctx) return;
        setFill(r, g, b, a);
        ctx.fillRect(x, y, w, h);
    }

    function canvas_fill_text(ptr, len, x, y, size, r, g, b, a, buffer) {
        if (!ctx) return;
        const text = loadStringRaw(buffer, ptr, len);
        setFill(r, g, b, a);
        setFont(size);
        ctx.fillText(text, x, y);
    }

    function canvas_measure_text(ptr, len, size, buffer) {
        if (!ctx) return 0;
        const text = loadStringRaw(buffer, ptr, len);
        const cacheKey = len <= 48 ? `${size}\x1f${text}` : "";
        if (cacheKey) {
            const cached = measureCache.get(cacheKey);
            if (cached !== undefined) return cached;
        }
        setFont(size);
        const width = ctx.measureText(text).width;
        if (cacheKey) {
            if (measureCache.size >= TEXT_MEASURE_CACHE_CAP) measureCache.clear();
            measureCache.set(cacheKey, width);
        }
        return width;
    }

    function canvas_line(x1, y1, x2, y2, r, g, b, a, thickness) {
        if (!ctx) return;
        setStroke(r, g, b, a);
        setLineWidth(thickness);
        ctx.beginPath();
        ctx.moveTo(x1, y1);
        ctx.lineTo(x2, y2);
        ctx.stroke();
    }

    function canvas_clip_push(x, y, w, h) {
        if (!ctx) return;
        ctx.save();
        ctx.beginPath();
        ctx.rect(x, y, w, h);
        ctx.clip();
    }

    function canvas_clip_pop() {
        if (!ctx) return;
        ctx.restore();
    }

    function canvas_width() {
        return canvas ? Math.round(canvas.width / dpr) : 0;
    }

    function canvas_height() {
        return canvas ? Math.round(canvas.height / dpr) : 0;
    }

    return {
        canvas,
        syncSize,
        isSizeDirty,
        // odin_env procs
        canvas_clear,
        canvas_fill_rect,
        canvas_fill_text,
        canvas_measure_text,
        canvas_line,
        canvas_clip_push,
        canvas_clip_pop,
        canvas_width,
        canvas_height,
    };
}
