package config

import (
	"fmt"
	"regexp"
)

// identifier matches the development token and tenant formats used by this
// demo: UUID-like token values and tenant names such as "tenant--dev".
// Restricting input to this alphabet means Go's %q output is also a valid Lua
// string literal, with no Lua-specific escaping code required.
var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)

// TenantConfig is the versioned input used to build the ECDS Lua filter.
type TenantConfig struct {
	Version                   int               `yaml:"version"`
	RemoveAuthorizationHeader bool              `yaml:"remove_authorization_header"`
	Tenants                   map[string]string `yaml:"tenants"`
}

func (c *TenantConfig) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("version must be 1, got %d", c.Version)
	}
	if c.Tenants == nil {
		// An omitted/null tenant map is deliberately equivalent to an empty map.
		c.Tenants = map[string]string{}
	}
	seenTokens := make(map[string]string)
	for tenant, token := range c.Tenants {
		if tenant == "" {
			return fmt.Errorf("tenant names must not be empty")
		}
		if !identifier.MatchString(tenant) {
			return fmt.Errorf("tenant %q must contain only letters, digits, and hyphens", tenant)
		}
		if token == "" {
			return fmt.Errorf("tokens must not be empty")
		}
		if !identifier.MatchString(token) {
			return fmt.Errorf("token %q must contain only letters, digits, and hyphens", token)
		}
		if previousTenant, exists := seenTokens[token]; exists {
			return fmt.Errorf("token %q is assigned to both %q and %q", token, previousTenant, tenant)
		}
		seenTokens[token] = tenant
	}
	return nil
}
