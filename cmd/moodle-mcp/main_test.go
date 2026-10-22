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

func TestAuthorizeListsAulesPlatforms(t *testing.T) {
	a := testApp(t)
	client, err := a.store.Register([]string{"https://client.example/callback"})
	if err != nil {
		t.Fatal(err)
	}
	challenge := strings.Repeat("a", 43)
	req := httptest.NewRequest(http.MethodGet, "/authorize?response_type=code&client_id="+client.ID+"&redirect_uri=https%3A%2F%2Fclient.example%2Fcallback&code_challenge="+challenge+"&code_challenge_method=S256", nil)
	recorder := httptest.NewRecorder()
	a.authorizeGet(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "name=\"moodle_url\"") || !strings.Contains(body, config.FPURL) {
		t.Fatal("Aules platform selector missing")
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

func TestChatGPTCORSPreflight(t *testing.T) {
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/mcp", nil)
	req.Header.Set("Origin", "https://chatgpt.com")
	req.Header.Set("Access-Control-Request-Headers", "authorization, content-type")
	chatGPTCORS(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("preflight reached protected handler")
	})).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d", recorder.Code)
	}
	if recorder.Header().Get("Access-Control-Allow-Origin") != "https://chatgpt.com" {
		t.Fatal("CORS origin missing")
	}
}

func TestIsMobileUserAgent(t *testing.T) {
	for _, userAgent := range []string{
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)",
		"Mozilla/5.0 (Linux; Android 15; Pixel 9) Mobile",
	} {
		if !isMobileUserAgent(userAgent) {
			t.Errorf("isMobileUserAgent(%q) = false", userAgent)
		}
	}
	if isMobileUserAgent("Mozilla/5.0 (X11; Linux x86_64) Gecko/20100101 Firefox/143.0") {
		t.Fatal("desktop user agent classified as mobile")
	}
}

func TestCallbackHandoffTemplateEscapesCallbackQuery(t *testing.T) {
	recorder := httptest.NewRecorder()
	callback := "https://chatgpt.com/callback?code=abc&state=xyz"
	if err := callbackHandoffTemplate.Execute(recorder, callbackPage{URL: callback}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(recorder.Body.String(), `href="https://chatgpt.com/callback?code=abc&amp;state=xyz"`) {
		t.Fatal("callback URL missing or not HTML-escaped")
	}
}

func TestLandingRejectsBaseURLAsMCPEndpoint(t *testing.T) {
	a := testApp(t)
	recorder := httptest.NewRecorder()
	a.landing(recorder, httptest.NewRequest(http.MethodPost, "/", nil))
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "invalid_mcp_endpoint") || !strings.Contains(recorder.Body.String(), "https://mcp.example/mcp") {
		t.Fatalf("landing response = %d %s", recorder.Code, recorder.Body.String())
	}
}
