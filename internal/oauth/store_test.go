package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"path/filepath"
	"testing"
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
	accessToken, _, gotResource, err := store.Exchange(code, client.ID, redirect, verifier, resource)
	if err != nil {
		t.Fatal(err)
	}
	if accessToken == "secret-aules-token" {
		t.Fatal("access token exposed Aules token")
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
