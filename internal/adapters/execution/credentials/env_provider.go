package credentials

import (
	"fmt"
	"os"
	"strings"
)

const (
	defaultAPIKeyEnv    = "MR_BINANCE_API_KEY"    // #nosec G101 -- env var name, not credential material.
	defaultAPISecretEnv = "MR_BINANCE_API_SECRET" // #nosec G101 -- env var name, not credential material.
)

// EnvProviderConfig configures environment variable names used for credentials.
type EnvProviderConfig struct {
	APIKeyEnv    string
	APISecretEnv string
}

// EnvProvider resolves trade-only credentials from environment variables.
type EnvProvider struct {
	apiKeyEnv    string
	apiSecretEnv string
}

func NewEnvProvider(cfg EnvProviderConfig) *EnvProvider {
	apiKeyEnv := strings.TrimSpace(cfg.APIKeyEnv)
	if apiKeyEnv == "" {
		apiKeyEnv = defaultAPIKeyEnv
	}
	apiSecretEnv := strings.TrimSpace(cfg.APISecretEnv)
	if apiSecretEnv == "" {
		apiSecretEnv = defaultAPISecretEnv
	}
	return &EnvProvider{
		apiKeyEnv:    apiKeyEnv,
		apiSecretEnv: apiSecretEnv,
	}
}

func (p *EnvProvider) ResolveTradeCredentialMaterial() (ProviderMaterial, error) {
	if p == nil {
		return ProviderMaterial{}, fmt.Errorf("trade credentials provider is nil")
	}
	apiKey := strings.TrimSpace(os.Getenv(p.apiKeyEnv))
	if apiKey == "" {
		return ProviderMaterial{}, fmt.Errorf("missing trade credential env var %s", p.apiKeyEnv)
	}
	apiSecret := strings.TrimSpace(os.Getenv(p.apiSecretEnv))
	if apiSecret == "" {
		return ProviderMaterial{}, fmt.Errorf("missing trade credential env var %s", p.apiSecretEnv)
	}
	return ProviderMaterial{
		Credentials: TradeCredentials{
			APIKey:    apiKey,
			APISecret: apiSecret,
		},
		Scope:           ScopeTradeOnly,
		TradeOnly:       true,
		ProviderID:      ProviderIDEnvStaticV1,
		SourceType:      SourceTypeEnv,
		SourceRef:       "execution.real.binance.trade_api",
		RevocationReady: true,
	}, nil
}
