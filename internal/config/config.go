package config

import (
	"errors"
	"net/url"
	"os"
	"strings"
)

const (
	SemiPresencialURL = "https://aules.edu.gva.es/semipresencial"
	FPURL             = "https://aules.edu.gva.es/fp"
	DefaultMoodleURL  = SemiPresencialURL
)

type MoodlePlatform struct {
	Name string
	URL  string
}

var MoodlePlatforms = []MoodlePlatform{
	{Name: "Semipresencial", URL: SemiPresencialURL},
	{Name: "Formación Profesional", URL: FPURL},
}

func IsSupportedMoodleURL(raw string) bool {
	raw = strings.TrimRight(raw, "/")
	for _, platform := range MoodlePlatforms {
		if raw == platform.URL {
			return true
		}
	}
	return false
}

type Config struct {
	PublicBaseURL    string
	ListenAddr       string
	DatabasePath     string
	EncryptionKey    string
	MoodleURL        string
	KeepaliveEnabled bool
}

func LoadFromEnv() Config {
	return Config{
		PublicBaseURL:    strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/"),
		ListenAddr:       envOr("LISTEN_ADDR", ":8080"),
		DatabasePath:     envOr("DATABASE_PATH", "/data/moodle-mcp.db"),
		EncryptionKey:    os.Getenv("AULES_ENCRYPTION_KEY"),
		MoodleURL:        strings.TrimRight(envOr("MOODLE_URL", DefaultMoodleURL), "/"),
		KeepaliveEnabled: strings.EqualFold(os.Getenv("AULES_KEEPALIVE_ENABLED"), "true"),
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func (c Config) Validate() error {
	if c.PublicBaseURL == "" || c.EncryptionKey == "" {
		return errors.New("PUBLIC_BASE_URL and AULES_ENCRYPTION_KEY are required")
	}
	for _, raw := range []string{c.PublicBaseURL, c.MoodleURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return errors.New("public and Moodle URLs must be valid HTTPS URLs")
		}
	}
	if !IsSupportedMoodleURL(c.MoodleURL) {
		return errors.New("MOODLE_URL must be a supported Aules platform")
	}
	return nil
}
