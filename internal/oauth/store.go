package oauth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db   *sql.DB
	aead cipher.AEAD
}

type Client struct {
	ID           string
	RedirectURIs []string
}

type Authorization struct {
	ClientID       string
	RedirectURI    string
	Challenge      string
	State          string
	EncryptedAules []byte
}

type AulesSession struct {
	Token     string
	MoodleURL string
}

func Open(path, encodedKey string) (*Store, error) {
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil || len(key) != 32 {
		return nil, errors.New("AULES_ENCRYPTION_KEY must be base64-encoded 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, aead: aead}
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;
CREATE TABLE IF NOT EXISTS clients (id TEXT PRIMARY KEY, redirect_uris TEXT NOT NULL, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS codes (hash BLOB PRIMARY KEY, client_id TEXT NOT NULL, redirect_uri TEXT NOT NULL, challenge TEXT NOT NULL, aules_token BLOB NOT NULL, moodle_url TEXT NOT NULL DEFAULT 'https://aules.edu.gva.es/semipresencial', expires_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS tokens (hash BLOB PRIMARY KEY, client_id TEXT NOT NULL, aules_token BLOB NOT NULL, moodle_url TEXT NOT NULL DEFAULT 'https://aules.edu.gva.es/semipresencial', expires_at INTEGER NOT NULL);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	for _, statement := range []string{
		"ALTER TABLE codes ADD COLUMN moodle_url TEXT NOT NULL DEFAULT 'https://aules.edu.gva.es/semipresencial'",
		"ALTER TABLE tokens ADD COLUMN moodle_url TEXT NOT NULL DEFAULT 'https://aules.edu.gva.es/semipresencial'",
	} {
		if _, err := db.Exec(statement); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			db.Close()
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Encrypt(plain []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return s.aead.Seal(nonce, nonce, plain, nil), nil
}

func (s *Store) Decrypt(data []byte) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(data) < n {
		return nil, errors.New("invalid ciphertext")
	}
	return s.aead.Open(nil, data[:n], data[n:], nil)
}

func randomToken(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func digest(value string) []byte { h := sha256.Sum256([]byte(value)); return h[:] }

func ValidateRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Fragment != "" || u.Scheme == "" || u.Host == "" {
		return errors.New("invalid redirect_uri")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1")) {
		return errors.New("redirect_uri must use https")
	}
	return nil
}

func (s *Store) Register(redirects []string) (*Client, error) {
	if len(redirects) == 0 {
		return nil, errors.New("redirect_uris required")
	}
	for _, uri := range redirects {
		if err := ValidateRedirectURI(uri); err != nil {
			return nil, err
		}
	}
	id, err := randomToken(24)
	if err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(redirects)
	_, err = s.db.Exec(`INSERT INTO clients(id,redirect_uris,created_at) VALUES(?,?,?)`, id, string(encoded), time.Now().Unix())
	if err != nil {
		return nil, err
	}
	return &Client{ID: id, RedirectURIs: redirects}, nil
}

func (s *Store) ValidateClient(id, redirect string) error {
	var raw string
	if err := s.db.QueryRow(`SELECT redirect_uris FROM clients WHERE id=?`, id).Scan(&raw); err != nil {
		return errors.New("unknown client")
	}
	var uris []string
	if json.Unmarshal([]byte(raw), &uris) != nil {
		return errors.New("invalid client")
	}
	for _, candidate := range uris {
		if candidate == redirect {
			return nil
		}
	}
	return errors.New("redirect_uri mismatch")
}

func ValidatePKCE(challenge, method string) error {
	if method != "S256" || len(challenge) < 43 || len(challenge) > 128 {
		return errors.New("PKCE S256 required")
	}
	if _, err := base64.RawURLEncoding.DecodeString(challenge); err != nil {
		return errors.New("invalid code_challenge")
	}
	return nil
}

func VerifyPKCE(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:]) == challenge
}

func (s *Store) CreateCode(clientID, redirect, challenge, moodleURL, aulesToken string) (string, error) {
	if err := s.ValidateClient(clientID, redirect); err != nil {
		return "", err
	}
	enc, err := s.Encrypt([]byte(aulesToken))
	if err != nil {
		return "", err
	}
	code, err := randomToken(32)
	if err != nil {
		return "", err
	}
	_, err = s.db.Exec(`INSERT INTO codes(hash,client_id,redirect_uri,challenge,aules_token,moodle_url,expires_at) VALUES(?,?,?,?,?,?,?)`, digest(code), clientID, redirect, challenge, enc, moodleURL, time.Now().Add(5*time.Minute).Unix())
	return code, err
}

func (s *Store) Exchange(code, clientID, redirect, verifier string) (string, int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()
	var gotClient, gotRedirect, challenge, moodleURL string
	var encrypted []byte
	var expires int64
	err = tx.QueryRow(`SELECT client_id,redirect_uri,challenge,aules_token,moodle_url,expires_at FROM codes WHERE hash=?`, digest(code)).Scan(&gotClient, &gotRedirect, &challenge, &encrypted, &moodleURL, &expires)
	if err != nil || expires < time.Now().Unix() || gotClient != clientID || gotRedirect != redirect || !VerifyPKCE(verifier, challenge) {
		return "", 0, errors.New("invalid_grant")
	}
	if _, err = tx.Exec(`DELETE FROM codes WHERE hash=?`, digest(code)); err != nil {
		return "", 0, err
	}
	token, err := randomToken(32)
	if err != nil {
		return "", 0, err
	}
	expires = time.Now().Add(time.Hour).Unix()
	if _, err = tx.Exec(`INSERT INTO tokens(hash,client_id,aules_token,moodle_url,expires_at) VALUES(?,?,?,?,?)`, digest(token), clientID, encrypted, moodleURL, expires); err != nil {
		return "", 0, err
	}
	if err = tx.Commit(); err != nil {
		return "", 0, err
	}
	return token, expires, nil
}

func (s *Store) AulesSession(bearer string) (AulesSession, error) {
	var encrypted []byte
	var moodleURL string
	var expires int64
	if err := s.db.QueryRow(`SELECT aules_token,moodle_url,expires_at FROM tokens WHERE hash=?`, digest(bearer)).Scan(&encrypted, &moodleURL, &expires); err != nil || expires < time.Now().Unix() {
		return AulesSession{}, errors.New("invalid token")
	}
	plain, err := s.Decrypt(encrypted)
	if err != nil {
		return AulesSession{}, err
	}
	return AulesSession{Token: string(plain), MoodleURL: moodleURL}, nil
}

func (s *Store) Revoke(token string) error {
	_, err := s.db.Exec(`DELETE FROM tokens WHERE hash=?`, digest(token))
	return err
}

func Bearer(header string) (string, error) {
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", fmt.Errorf("missing bearer token")
	}
	return parts[1], nil
}
