package config

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAppConfigSourceLoad(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		w.Header().Set("Configuration-Version", "7")
		_, _ = io.WriteString(w, "version: 1\ntenants:\n  mytest--dev: token-a\n")
	}))
	defer server.Close()

	source := NewAppConfigSource(context.Background(), server.URL, slog.New(slog.NewTextHandler(io.Discard, nil)))
	cfg, err := source.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tenants["mytest--dev"] != "token-a" {
		t.Fatalf("tenant token = %q, want token-a", cfg.Tenants["mytest--dev"])
	}
	if source.version != "7" {
		t.Fatalf("version = %q, want 7", source.version)
	}
}
