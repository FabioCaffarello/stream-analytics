// perf.js — Performance HUD overlay and idle throttle constants.
//
// The perf HUD is debug-only (PERF_HUD_ENABLED = false by default).
// Idle throttle constants are exported for the frame loop.

export const PERF_HUD_ENABLED = false;
export const IDLE_STEP_FPS = 15;
export const IDLE_STEP_INTERVAL_MS = IDLE_STEP_FPS > 0 ? (1000 / IDLE_STEP_FPS) : 0;
export const IDLE_QUIET_MS = 250;

export function initPerfHud() {
    let hudEl = null;
    const state = {
        rafFps: 0,
        rafFrames: 0,
        rafWindowStart: 0,
        stepFps: 0,
        stepCalls: 0,
        stepWindowStart: 0,
        lastStepDtMs: 0,
        stepDtAvgMs: 0,
        stepDtSamples: 0,
        lastStepCpuMs: 0,
        stepCpuAvgMs: 0,
        stepCpuMaxMs: 0,
        idleSkips: 0,
        idleSkipsWindow: 0,
        lastHudPaint: 0,
        lastWsPerfLine: "",
        lastWsPerfTs: 0,
    };

    function ensure() {
        if (!PERF_HUD_ENABLED || hudEl) return;
        hudEl = document.createElement("div");
        hudEl.id = "mr-perf-hud";
        Object.assign(hudEl.style, {
            position: "fixed",
            top: "8px",
            right: "8px",
            zIndex: "9999",
            minWidth: "240px",
            maxWidth: "min(42vw, 420px)",
            padding: "8px 10px",
            background: "rgba(7, 10, 14, 0.82)",
            border: "1px solid rgba(120, 180, 255, 0.22)",
            borderRadius: "8px",
            color: "#c8f2ff",
            font: "12px/1.35 ui-monospace, SFMono-Regular, Menlo, monospace",
            whiteSpace: "pre-wrap",
            pointerEvents: "none",
            backdropFilter: "blur(6px)",
            boxShadow: "0 6px 24px rgba(0,0,0,0.28)",
        });
        document.body.appendChild(hudEl);
    }

    function consumeConsoleLine(line) {
        if (!PERF_HUD_ENABLED || typeof line !== "string") return;
        if (!line.startsWith("[ws-perf] ")) return;
        state.lastWsPerfLine = line;
        state.lastWsPerfTs = performance.now();
    }

    function onFrame(now, wsBridge, canvasMod) {
        if (!PERF_HUD_ENABLED) return;
        ensure();
        if (!state.rafWindowStart) state.rafWindowStart = now;
        state.rafFrames += 1;
        const rafDt = now - state.rafWindowStart;
        if (rafDt >= 1000) {
            state.rafFps = (state.rafFrames * 1000) / rafDt;
            state.rafFrames = 0;
            state.rafWindowStart = now;
        }
        if (now - state.lastHudPaint < 250) return;
        state.lastHudPaint = now;

        const wsStateLabel =
            wsBridge.getState() === 2 ? "open" :
            wsBridge.getState() === 1 ? "connecting" :
            wsBridge.getState() === 3 ? "closing" : "closed";
        const wsPerfAgeMs = state.lastWsPerfTs > 0 ? Math.round(now - state.lastWsPerfTs) : -1;
        const lines = [
            "PERF HUD",
            `raf_fps: ${state.rafFps.toFixed(1)}`,
            `step_fps: ${state.stepFps.toFixed(1)} dt_ms=${state.lastStepDtMs.toFixed(1)} avg=${state.stepDtAvgMs.toFixed(1)} idle_skips=${state.idleSkips}`,
            `step_cpu_ms: ${state.lastStepCpuMs.toFixed(2)} avg=${state.stepCpuAvgMs.toFixed(2)} max=${state.stepCpuMaxMs.toFixed(2)}`,
            `ws: ${wsStateLabel} queue=${wsBridge.getQueueLength()} drop=${wsBridge.getDropCount()}`,
            `canvas: ${canvasMod.canvas ? `${canvasMod.canvas.width}x${canvasMod.canvas.height}` : "n/a"} dirty=${canvasMod.isSizeDirty() ? 1 : 0}`,
        ];
        if (state.lastWsPerfLine) {
            lines.push(`ws-perf age_ms: ${wsPerfAgeMs}`);
            lines.push(state.lastWsPerfLine);
        } else {
            lines.push("ws-perf: waiting telemetry");
        }
        if (hudEl) hudEl.textContent = lines.join("\n");
    }

    function onStep(now, dtSec, cpuMs) {
        if (!PERF_HUD_ENABLED) return;
        if (!state.stepWindowStart) state.stepWindowStart = now;
        state.stepCalls += 1;
        const dtMs = Math.max(0, dtSec * 1000);
        state.lastStepDtMs = dtMs;
        const stepCpuMs = Math.max(0, cpuMs || 0);
        state.lastStepCpuMs = stepCpuMs;
        if (state.stepDtSamples <= 0) {
            state.stepDtAvgMs = dtMs;
            state.stepCpuAvgMs = stepCpuMs;
            state.stepCpuMaxMs = stepCpuMs;
            state.stepDtSamples = 1;
        } else {
            state.stepDtAvgMs = state.stepDtAvgMs * 0.9 + dtMs * 0.1;
            state.stepCpuAvgMs = state.stepCpuAvgMs * 0.9 + stepCpuMs * 0.1;
            if (stepCpuMs > state.stepCpuMaxMs) state.stepCpuMaxMs = stepCpuMs;
            state.stepDtSamples += 1;
        }
        const stepWindowDt = now - state.stepWindowStart;
        if (stepWindowDt >= 1000) {
            state.stepFps = (state.stepCalls * 1000) / stepWindowDt;
            state.stepCalls = 0;
            state.stepWindowStart = now;
            state.idleSkips = state.idleSkipsWindow;
            state.idleSkipsWindow = 0;
            state.stepCpuMaxMs = state.lastStepCpuMs;
        }
    }

    function recordIdleSkip() {
        if (PERF_HUD_ENABLED) state.idleSkipsWindow += 1;
    }

    return { consumeConsoleLine, onFrame, onStep, recordIdleSkip };
}
