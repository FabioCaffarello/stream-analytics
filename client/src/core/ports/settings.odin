package ports

// Settings port — platform-injected persistent key-value storage.
// Core declares what to save; platform handles I/O.
//
// Native: backed by ~/.market-raccoon.json file.
// Web:    backed by localStorage (future), stub for now.

Settings_Port :: struct {
	load:  proc(key: string) -> (value: string, ok: bool),
	save:  proc(key: string, value: string) -> bool,
	flush: proc(), // Force write to disk (native) or localStorage (web).
	clipboard_write: proc(text: string) -> bool, // Copy text to system clipboard (nil = unsupported).
	// S126: Backend workspace persistence.
	backend_load: proc() -> bool, // Load workspace from backend into local store. Returns true if applied.
	backend_sync: proc() -> bool, // Sync local workspace state to backend. Returns true on success.
}
