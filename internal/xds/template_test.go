package xds

import (
	"strings"
	"testing"

	"envoy-xds-lua-demo/internal/config"
)

func TestRenderLua(t *testing.T) {
	cfg := &config.TenantConfig{Version: 1, Tenants: map[string]string{"tenant-b": "token-b", "tenant-a": "token-a"}}
	got, err := RenderLua(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `["token-a"] = "tenant-a"`) {
		t.Fatalf("mapping missing:\n%s", got)
	}
	if strings.Index(got, "token-a") > strings.Index(got, "token-b") {
		t.Fatal("output is not sorted")
	}
}

func TestTemplateRejectsUnsafeValues(t *testing.T) {
	_, err := RenderLua(&config.TenantConfig{Version: 1, Tenants: map[string]string{"tenant": "a\"b"}})
	if err == nil {
		t.Fatal("expected unsafe token to be rejected")
	}
}

func TestTemplateAuthorizationRemoval(t *testing.T) {
	cfg := &config.TenantConfig{Version: 1, Tenants: map[string]string{}}
	without, err := RenderLua(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(without, `headers():remove`) {
		t.Fatal("removal present when disabled")
	}
	cfg.RemoveAuthorizationHeader = true
	with, err := RenderLua(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(with, `headers():remove("authorization")`) {
		t.Fatal("removal absent when enabled")
	}
}
