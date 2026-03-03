package app

// RuleConfig is shared configuration for all evidence rules.
type RuleConfig struct {
	MaxStreams int   // hard cap on per-stream state entries, default 256
	CooldownMs int64 // min ms between emissions per stream, default 5000
}

// DefaultRuleConfig returns production defaults.
func DefaultRuleConfig() RuleConfig {
	return RuleConfig{
		MaxStreams: 256,
		CooldownMs: 5000,
	}
}

// streamEntry tracks per-stream cooldown state.
type streamEntry struct {
	lastEmitTs int64
}

// canEmit returns true if enough time has passed since last emission.
func (s *streamEntry) canEmit(tsServer int64, cooldownMs int64) bool {
	return tsServer-s.lastEmitTs >= cooldownMs
}
