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
}
