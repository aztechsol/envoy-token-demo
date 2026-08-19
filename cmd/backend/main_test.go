package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerReturnsRequestMetadata(t *testing.T) {
	body := []byte(`{"message":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "http://backend/test?ignored=true", bytes.NewReader(body))
	req.Header.Set("X-Scope-OrgID", "tenant-a")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	newHandler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var got response
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := response{
		Method:               http.MethodPost,
		Path:                 "/test",
		Tenant:               "tenant-a",
		AuthorizationPresent: false,
		ContentType:          "application/json",
		ContentLength:        int64(len(body)),
	}
	if got != want {
		t.Errorf("response = %#v, want %#v", got, want)
	}
}

func TestHandlerReportsAuthorizationPresenceWithoutReturningValue(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://backend/inspect", nil)
	req.Header.Set("Authorization", "Bearer secret-that-must-not-be-returned")
	recorder := httptest.NewRecorder()

	newHandler().ServeHTTP(recorder, req)

	rawResponse := append([]byte(nil), recorder.Body.Bytes()...)
	var got response
	if err := json.Unmarshal(rawResponse, &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !got.AuthorizationPresent {
		t.Error("authorization_present = false, want true")
	}
	if bytes.Contains(rawResponse, []byte("secret-that-must-not-be-returned")) {
		t.Error("response exposed the bearer token")
	}
}

func TestHandlerTreatsEmptyAuthorizationHeaderAsPresent(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://backend/inspect", nil)
	req.Header["Authorization"] = []string{""}
	recorder := httptest.NewRecorder()

	newHandler().ServeHTTP(recorder, req)

	var got response
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !got.AuthorizationPresent {
		t.Error("authorization_present = false, want true when the header exists with an empty value")
	}
}

func TestHandlerPreservesUnknownContentLength(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://backend/stream", bytes.NewReader([]byte("stream")))
	req.ContentLength = -1
	recorder := httptest.NewRecorder()

	newHandler().ServeHTTP(recorder, req)

	var got response
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ContentLength != -1 {
		t.Errorf("content_length = %d, want -1 for an unknown/chunked request length", got.ContentLength)
	}
}
