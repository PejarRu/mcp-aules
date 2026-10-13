package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PejarRu/mcp-aules/internal/config"
	"github.com/PejarRu/mcp-aules/internal/oauth"
)

func testApp(t *testing.T) *app {
	t.Helper()
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	store, err := oauth.Open(filepath.Join(t.TempDir(), "app.db"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return &app{cfg: config.Config{PublicBaseURL: "https://mcp.example", MoodleURL: config.DefaultMoodleURL}, store: store}
}

func TestHealth(t *testing.T) {
	recorder := httptest.NewRecorder()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestOAuthMetadata(t *testing.T) {
	a := testApp(t)
	recorder := httptest.NewRecorder()
	a.authorizationMetadata(recorder, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var metadata map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["code_challenge_methods_supported"].([]any)[0] != "S256" {
		t.Fatal("S256 missing from metadata")
	}
}

func TestBearerChallenge(t *testing.T) {
	a := testApp(t)
	recorder := httptest.NewRecorder()
	a.bearer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("protected handler called")
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}")))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", recorder.Code)
	}
	if !strings.Contains(recorder.Header().Get("WWW-Authenticate"), "resource_metadata=") {
		t.Fatal("resource metadata challenge missing")
	}
}
