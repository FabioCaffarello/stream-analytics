package bootstrap

import (
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/problem"
)

// ConfigOverride mutates an AppConfig after loading but before validation.
// Use this to apply CLI flag overrides.
type ConfigOverride func(*config.AppConfig)

// LoadAndValidate loads a JSONC config file, applies overrides, and validates.
// If path is empty, defaults are used.  Returns a fully validated AppConfig
// or the first problem encountered.
func LoadAndValidate(path string, overrides ...ConfigOverride) (config.AppConfig, *problem.Problem) {
	cfg, prob := config.Load(path)
	if prob != nil {
		return config.AppConfig{}, prob
	}
	for _, fn := range overrides {
		fn(&cfg)
	}
	if prob = cfg.Validate(); prob != nil {
		return config.AppConfig{}, prob
	}
	return cfg, nil
}
