package xds

import (
	"bytes"
	_ "embed"
	"fmt"
	"sort"
	"text/template"

	"envoy-xds-lua-demo/internal/config"
)

//go:embed tenant_auth.lua.tmpl
var tenantAuthTemplate string

var parsedTenantAuthTemplate = template.Must(template.New("tenant_auth.lua.tmpl").
	Parse(tenantAuthTemplate))

// RenderLua renders Envoy's dynamic Lua configuration from Go's standard
// text/template. Config validation limits values to an identifier alphabet, so
// the template's built-in printf %q produces a valid Lua string literal.
func RenderLua(cfg *config.TenantConfig) (string, error) {
	if err := cfg.Validate(); err != nil {
		return "", err
	}

	data := struct {
		Tokens                    []templateToken
		RemoveAuthorizationHeader bool
	}{
		Tokens:                    make([]templateToken, 0, len(cfg.Tenants)),
		RemoveAuthorizationHeader: cfg.RemoveAuthorizationHeader,
	}
	for tenant, token := range cfg.Tenants {
		data.Tokens = append(data.Tokens, templateToken{Token: token, Tenant: tenant})
	}
	sort.Slice(data.Tokens, func(i, j int) bool { return data.Tokens[i].Token < data.Tokens[j].Token })

	var rendered bytes.Buffer
	if err := parsedTenantAuthTemplate.Execute(&rendered, data); err != nil {
		return "", fmt.Errorf("render Lua template: %w", err)
	}
	return rendered.String(), nil
}

type templateToken struct {
	Token  string
	Tenant string
}
