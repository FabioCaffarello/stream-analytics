package wsserver

import (
	"net/http"
	"slices"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/market-raccoon/internal/shared/problem"
)

const wsScopeRead = "read"

// Principal is the authenticated websocket identity.
type Principal struct {
	ClientID string
	TenantID string
	Scopes   []string
	APIKey   string
}

func (p Principal) HasScope(scope string) bool {
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" {
		return false
	}
	for _, cur := range p.Scopes {
		if strings.EqualFold(strings.TrimSpace(cur), scope) {
			return true
		}
	}
	return false
}

// JWTAuthConfig controls optional HMAC JWT authentication.
type JWTAuthConfig struct {
	Enabled     bool
	HS256Secret string
	Issuer      string
	Audience    string
}

// AuthConfig controls websocket API-key/JWT authentication.
type AuthConfig struct {
	Enabled      bool
	APIKeys      map[string]string   // api_key -> client_id
	APIKeyScopes map[string][]string // api_key -> scopes
	JWT          JWTAuthConfig
}

func (cfg AuthConfig) Authenticate(r *http.Request) (Principal, *problem.Problem) {
	if !cfg.Enabled {
		return Principal{
			ClientID: "anonymous",
			Scopes:   []string{wsScopeRead},
		}, nil
	}
	key := ""
	if r != nil {
		key = strings.TrimSpace(r.Header.Get("X-API-Key"))
		// Fallback: browser WebSocket API cannot set custom headers,
		// so accept api_key as a query parameter.
		if key == "" {
			key = strings.TrimSpace(r.URL.Query().Get("api_key"))
		}
	}
	if key != "" {
		clientID, ok := cfg.APIKeys[key]
		if !ok {
			return Principal{}, problem.New(problem.ValidationFailed, "invalid API key")
		}
		return Principal{
			ClientID: strings.TrimSpace(clientID),
			Scopes:   normalizeScopes(cfg.APIKeyScopes[key]),
			APIKey:   key,
		}, nil
	}

	if cfg.JWT.Enabled {
		return cfg.authenticateJWT(r)
	}
	return Principal{}, problem.New(problem.ValidationFailed, "missing API key")
}

func normalizeScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return []string{wsScopeRead}
	}
	out := make([]string, 0, len(scopes))
	for _, raw := range scopes {
		scope := strings.ToLower(strings.TrimSpace(raw))
		if scope == "" {
			continue
		}
		out = append(out, scope)
	}
	if len(out) == 0 {
		return []string{wsScopeRead}
	}
	slices.Sort(out)
	out = slices.Compact(out)
	return out
}

func (cfg AuthConfig) authenticateJWT(r *http.Request) (Principal, *problem.Problem) {
	tokenString, p := bearerTokenFromRequest(r)
	if p != nil {
		return Principal{}, p
	}
	secret := strings.TrimSpace(cfg.JWT.HS256Secret)
	if secret == "" {
		return Principal{}, problem.New(problem.Internal, "jwt secret not configured")
	}
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, problem.Newf(problem.ValidationFailed, "unsupported jwt alg %q", token.Method.Alg())
		}
		return []byte(secret), nil
	})
	if err != nil || token == nil || !token.Valid {
		return Principal{}, problem.New(problem.ValidationFailed, "invalid bearer token")
	}
	if p := cfg.validateJWTClaims(claims); p != nil {
		return Principal{}, p
	}
	return principalFromJWTClaims(claims)
}

func (cfg AuthConfig) validateJWTClaims(claims jwt.MapClaims) *problem.Problem {
	if issuer := strings.TrimSpace(cfg.JWT.Issuer); issuer != "" {
		if !jwtClaimMatches(claims["iss"], issuer) {
			return problem.New(problem.ValidationFailed, "invalid token issuer")
		}
	}
	if audience := strings.TrimSpace(cfg.JWT.Audience); audience != "" {
		if !jwtClaimMatches(claims["aud"], audience) {
			return problem.New(problem.ValidationFailed, "invalid token audience")
		}
	}
	return nil
}

func principalFromJWTClaims(claims jwt.MapClaims) (Principal, *problem.Problem) {
	clientID := strings.TrimSpace(claimString(claims, "client_id"))
	if clientID == "" {
		clientID = strings.TrimSpace(claimString(claims, "sub"))
	}
	if clientID == "" {
		return Principal{}, problem.New(problem.ValidationFailed, "missing jwt client identity")
	}
	tenantID := strings.TrimSpace(claimString(claims, "tenant_id"))
	scopes := extractJWTScopes(claims)
	return Principal{
		ClientID: clientID,
		TenantID: tenantID,
		Scopes:   normalizeScopes(scopes),
	}, nil
}

func bearerTokenFromRequest(r *http.Request) (string, *problem.Problem) {
	if r == nil {
		return "", problem.New(problem.ValidationFailed, "missing request")
	}
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if authHeader == "" {
		return "", problem.New(problem.ValidationFailed, "missing bearer token")
	}
	const bearerPrefix = "Bearer "
	if !strings.HasPrefix(strings.ToLower(authHeader), strings.ToLower(bearerPrefix)) {
		return "", problem.New(problem.ValidationFailed, "invalid authorization header")
	}
	tokenString := strings.TrimSpace(authHeader[len(bearerPrefix):])
	if tokenString == "" {
		return "", problem.New(problem.ValidationFailed, "missing bearer token")
	}
	return tokenString, nil
}

func claimString(claims jwt.MapClaims, key string) string {
	raw, ok := claims[key]
	if !ok || raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	default:
		return ""
	}
}

func extractJWTScopes(claims jwt.MapClaims) []string {
	scopeStr := strings.TrimSpace(claimString(claims, "scope"))
	if scopeStr != "" {
		return strings.Fields(scopeStr)
	}
	raw, ok := claims["scopes"]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				continue
			}
			out = append(out, s)
		}
		return out
	case []string:
		return append([]string(nil), v...)
	default:
		return nil
	}
}

func jwtClaimMatches(raw any, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return true
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v) == expected
	case []any:
		for _, item := range v {
			s, ok := item.(string)
			if ok && strings.TrimSpace(s) == expected {
				return true
			}
		}
		return false
	case []string:
		for _, s := range v {
			if strings.TrimSpace(s) == expected {
				return true
			}
		}
		return false
	default:
		return false
	}
}
