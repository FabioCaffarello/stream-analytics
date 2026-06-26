// memory.js — Shared WASM memory helpers.
//
// Provides text encoding/decoding for the WASM ↔ JS boundary.
// The wasmRef object is mutated after instantiation to hold exports/memory.

const TEXT_DECODER = new TextDecoder();
const TEXT_ENCODER = new TextEncoder();

export function loadStringRaw(buffer, ptr, len) {
    return TEXT_DECODER.decode(new Uint8Array(buffer, ptr, len));
}

export function encodeString(str) {
    return TEXT_ENCODER.encode(str);
}

// Mutable ref — closures read .memory after WASM instantiation.
export const wasmRef = { exports: null, memory: null };
