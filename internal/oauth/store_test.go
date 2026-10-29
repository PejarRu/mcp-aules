package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"path/filepath"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	store, err := Open(filepath.Join(t.TempDir(), "oauth.db"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func challenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func TestKeepaliveSessionsOnlyReturnsUnexpiredBearerGrants(t *testing.T) {
	store := testStore(t)
	redirect := "https://client.example/callback"
	client, err := store.Register([]string{redirect})
	if err != nil {
		t.Fatal(err)
	}
	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
	resource := "https://mcp.example/mcp"
	code, err := store.CreateCode(client.ID, redirect, challenge(verifier), "https://aules.edu.gva.es/fp", resource, "secret-token")
	if err != nil {
		t.Fatal(err)
	}
	bearer, _, _, err := store.Exchange(code, client.ID, redirect, verifier, resource)
	if err != nil {
		t.Fatal(err)
	}

	sessions, err := store.KeepaliveSessions()
	if err != nil || len(sessions) != 1 {
		t.Fatalf("KeepaliveSessions() = %d sessions, %v", len(sessions), err)
	}
	if sessions[0].Token != "secret-token" || sessions[0].MoodleURL != "https://aules.edu.gva.es/fp" {
		t.Fatalf("unexpected keepalive session: %#v", sessions[0])
	}
	if err := store.Revoke(bearer); err != nil {
		t.Fatal(err)
	}
	sessions, err = store.KeepaliveSessions()
	if err != nil || len(sessions) != 0 {
		t.Fatalf("revoked grant included: %d sessions, %v", len(sessions), err)
	}

	code, err = store.CreateCode(client.ID, redirect, challenge(verifier), "https://aules.edu.gva.es/fp", resource, "expired-token")
	if err != nil {
		t.Fatal(err)
	}
	bearer, _, _, err = store.Exchange(code, client.ID, redirect, verifier, resource)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE tokens SET expires_at=0 WHERE hash=?`, digest(bearer)); err != nil {
		t.Fatal(err)
	}
	sessions, err = store.KeepaliveSessions()
	if err != nil || len(sessions) != 0 {
		t.Fatalf("expired grant included: %d sessions, %v", len(sessions), err)
	}
}

func TestAuthorizationCodeFlow(t *testing.T) {
	store := testStore(t)
	redirect := "https://client.example/callback"
	client, err := store.Register([]string{redirect})
	if err != nil {
		t.Fatal(err)
	}
	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
	resource := "https://mcp.example/mcp"
	code, err := store.CreateCode(client.ID, redirect, challenge(verifier), "https://aules.edu.gva.es/fp", resource, "secret-aules-token")
	if err != nil {
		t.Fatal(err)
	}
	accessToken, expires, gotResource, err := store.Exchange(code, client.ID, redirect, verifier, resource)
	if err != nil {
		t.Fatal(err)
	}
	if accessToken == "secret-aules-token" {
		t.Fatal("access token exposed Aules token")
	}
	if seconds := expires - time.Now().Unix(); seconds < 6*24*60*60 || seconds > 7*24*60*60 {
		t.Fatalf("bearer lifetime = %d seconds; want about seven days", seconds)
	}
	if gotResource != resource {
		t.Fatalf("resource = %q", gotResource)
	}
	session, err := store.AulesSession(accessToken)
	if err != nil || session.Token != "secret-aules-token" || session.MoodleURL != "https://aules.edu.gva.es/fp" {
		t.Fatalf("AulesSession() = %#v, %v", session, err)
	}
	if _, _, _, err := store.Exchange(code, client.ID, redirect, verifier, resource); err == nil {
		t.Fatal("authorization code was reusable")
	}
	if err := store.Revoke(accessToken); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AulesSession(accessToken); err == nil {
		t.Fatal("revoked token remained valid")
	}
}

func TestAuthorizationIsolationAndPKCE(t *testing.T) {
	store := testStore(t)
	first, _ := store.Register([]string{"https://one.example/callback"})
	second, _ := store.Register([]string{"https://two.example/callback"})
	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
	code, err := store.CreateCode(first.ID, "https://one.example/callback", challenge(verifier), "https://aules.edu.gva.es/semipresencial", "https://mcp.example/mcp", "first-user")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.Exchange(code, second.ID, "https://two.example/callback", verifier, "https://mcp.example/mcp"); err == nil {
		t.Fatal("code accepted for different client")
	}
	if _, _, _, err := store.Exchange(code, first.ID, "https://one.example/callback", verifier+"wrong", "https://mcp.example/mcp"); err == nil {
		t.Fatal("code accepted wrong verifier")
	}
}

func TestValidation(t *testing.T) {
	if err := ValidatePKCE(challenge("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"), "S256"); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePKCE("plain", "plain"); err == nil {
		t.Fatal("plain PKCE accepted")
	}
	if err := ValidateRedirectURI("http://example.com/callback"); err == nil {
		t.Fatal("insecure redirect accepted")
	}
	if err := ValidateRedirectURI("http://localhost:3000/callback"); err != nil {
		t.Fatal(err)
	}
	if got, err := Bearer("Bearer token"); err != nil || got != "token" {
		t.Fatalf("Bearer() = %q, %v", got, err)
	}
}
