package config

import (
	"errors"
	"net/url"
	"os"
	"strings"
)

const DefaultMoodleURL = "https://aules.edu.gva.es/semipresencial"

type Config struct {
	PublicBaseURL string
	ListenAddr    string
	DatabasePath  string
	EncryptionKey string
	MoodleURL     string
}

func LoadFromEnv() Config {
	return Config{
		PublicBaseURL: strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/"),
		ListenAddr:    envOr("LISTEN_ADDR", ":8080"),
		DatabasePath:  envOr("DATABASE_PATH", "/data/moodle-mcp.db"),
		EncryptionKey: os.Getenv("AULES_ENCRYPTION_KEY"),
		MoodleURL:     envOr("MOODLE_URL", DefaultMoodleURL),
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
	if c.MoodleURL != DefaultMoodleURL {
		return errors.New("MOODLE_URL must be https://aules.edu.gva.es/semipresencial")
	}
	return nil
}
