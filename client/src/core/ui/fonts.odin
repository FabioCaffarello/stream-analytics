package ui

// Font identity tokens. Widgets reference fonts by id; platform maps to
// concrete font objects (ImFont on native, CSS font-family on web).
// Actual font loading is deferred to a platform-specific init step.

Font_Id :: enum u8 {
	Default = 0, // UI proportional (or system default)
	Mono,        // Monospace (code, tables, numbers)
	Bold,        // Emphasis
}

// Size tokens (re-exported from styles.odin for convenience).
// These are the canonical sizes; widgets should use these rather than
// ad-hoc f32 literals.
FONT_SIZE_XS :: f32(11)
FONT_SIZE_SM :: f32(13)
FONT_SIZE_MD :: f32(16) // alias for FONT_SIZE_BASE
