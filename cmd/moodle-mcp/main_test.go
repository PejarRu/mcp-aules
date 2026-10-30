package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PejarRu/mcp-aules/internal/config"
	"github.com/PejarRu/mcp-aules/internal/oauth"
	mcpserver "github.com/mark3labs/mcp-go/server"
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

func TestAuthenticatedMCPReconnectAndToolsList(t *testing.T) {
	a := testApp(t)
	redirect := "https://client.example/callback"
	client, err := a.store.Register([]string{redirect})
	if err != nil {
		t.Fatal(err)
	}
	verifier := strings.Repeat("a", 43)
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])
	resource := "https://mcp.example/mcp"
	code, err := a.store.CreateCode(client.ID, redirect, challenge, config.FPURL, resource, "fake-moodle-token")
	if err != nil {
		t.Fatal(err)
	}
	bearer, _, _, err := a.store.Exchange(code, client.ID, redirect, verifier, resource)
	if err != nil {
		t.Fatal(err)
	}
	handler := a.requestLog(a.bearer(mcpserver.NewStreamableHTTPServer(newMCPServer(), mcpserver.WithStateLess(true))))
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, req)
		return r
	}
	for i := 0; i < 2; i++ {
		init := post(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`)
		if init.Code != http.StatusOK || strings.Contains(init.Body.String(), `"error"`) {
			t.Fatalf("initialize: %d %s", init.Code, init.Body.String())
		}
		list := post(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
		if list.Code != http.StatusOK {
			t.Fatalf("tools/list: %d", list.Code)
		}
		for _, name := range []string{"read_forum", "read_resource", "get_recent_changes"} {
			if !strings.Contains(list.Body.String(), `"name":"`+name+`"`) {
				t.Fatalf("missing %s", name)
			}
		}
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

func TestMCPRequestLogRecordsStatusAndDuration(t *testing.T) {
	var logged strings.Builder
	oldWriter := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(oldWriter) })

	handler := (&app{}).requestLog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	req := httptest.NewRequest(http.MethodPost, "/mcp?token=must-not-log", nil)
	req.Header.Set("Authorization", "Bearer must-not-log")
	req.Header.Set("Mcp-Protocol-Version", "2025-11-25")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	got := logged.String()
	if !strings.Contains(got, "status=400") || !strings.Contains(got, `protocol_version="2025-11-25"`) {
		t.Fatalf("missing request diagnostics: %s", got)
	}
	if strings.Contains(got, "must-not-log") {
		t.Fatalf("sensitive request data was logged: %s", got)
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
