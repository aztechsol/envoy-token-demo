package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

type response struct {
	Method               string `json:"method"`
	Path                 string `json:"path"`
	Tenant               string `json:"tenant"`
	AuthorizationPresent bool   `json:"authorization_present"`
	ContentType          string `json:"content_type"`
	ContentLength        int64  `json:"content_length"`
}

func newHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Deliberately inspect metadata only. In particular, do not read the body:
		// OTLP and other streaming or binary request bodies remain untouched.
		authorizationPresent := len(r.Header.Values("Authorization")) > 0
		result := response{
			Method:               r.Method,
			Path:                 r.URL.Path,
			Tenant:               r.Header.Get("X-Scope-OrgID"),
			AuthorizationPresent: authorizationPresent,
			ContentType:          r.Header.Get("Content-Type"),
			ContentLength:        r.ContentLength,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(result); err != nil {
			// The error contains no request headers, so bearer credentials cannot be
			// exposed through this log path.
			log.Printf("encode response: %v", err)
		}
	})
}

func main() {
	server := &http.Server{
		Addr:              ":8081",
		Handler:           newHandler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("backend listening address=%s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("backend server failed: %v", err)
	}
}
