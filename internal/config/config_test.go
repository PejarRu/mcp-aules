package config_test

import (
	"testing"

	"github.com/PejarRu/mcp-aules/internal/config"
)

func validConfig() config.Config {
	return config.Config{
		PublicBaseURL: "https://anton.aules.mcp.development.antonberzins.com",
		ListenAddr:    ":8080",
		DatabasePath:  "/tmp/moodle-mcp.db",
		EncryptionKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		MoodleURL:     config.DefaultMoodleURL,
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*config.Config)
		wantErr bool
	}{
		{"valid", func(*config.Config) {}, false},
		{"missing public URL", func(c *config.Config) { c.PublicBaseURL = "" }, true},
		{"missing encryption key", func(c *config.Config) { c.EncryptionKey = "" }, true},
		{"insecure public URL", func(c *config.Config) { c.PublicBaseURL = "http://example.com" }, true},
		{"FP", func(c *config.Config) { c.MoodleURL = config.FPURL }, false},
		{"unsupported Moodle", func(c *config.Config) { c.MoodleURL = "https://moodle.example.com" }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.change(&cfg)
			if got := cfg.Validate(); (got != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", got, tt.wantErr)
			}
		})
	}
}
