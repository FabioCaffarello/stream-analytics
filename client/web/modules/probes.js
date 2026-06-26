// probes.js — Dynamic probe table builder for Playwright/web gate integration.
//
// Iterates wasm.exports keys matching /^probe_/ and builds the probe object
// automatically. New Odin probe exports are discovered without JS changes.

export function buildProbeTable(exports) {
    const table = {};
    for (const key in exports) {
        if (key.startsWith("probe_") && typeof exports[key] === "function") {
            // Convert probe_widget_trades_count -> camelCase key: widgetTradesCount
            const suffix = key.slice(6); // strip "probe_"
            const camel = suffix.replace(/_([a-z0-9])/g, (_, c) => c.toUpperCase());
            table[camel] = exports[key];
        }
    }
    return table;
}

export function installProbeHooks(exports) {
    const probeTable = buildProbeTable(exports);
    window.__mr_wasm_exports = exports;
    window.__mr_widget_probe = () => {
        const result = {};
        for (const key in probeTable) {
            result[key] = probeTable[key]();
        }
        return result;
    };
}
