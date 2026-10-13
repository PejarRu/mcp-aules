package tools

import (
	"testing"
	"time"
)

// --- truncate ---

func TestTruncate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		max   int
		want  string
	}{
		{"short string untouched", "hello", 10, "hello"},
		{"exact length untouched", "hello", 5, "hello"},
		{"truncated ASCII", "hello world", 5, "hello..."},
		{"empty string", "", 10, ""},
		{"multibyte UTF-8 preserved", "こんにちは世界", 5, "こんにちは..."},
		{"multibyte UTF-8 untouched", "こんにちは", 10, "こんにちは"},
		{"truncate at rune boundary", "café", 3, "caf..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.max)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
			}
		})
	}
}

// --- stripHTML ---

func TestStripHTML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text unchanged", "hello world", "hello world"},
		{"simple tag removed", "<p>hello</p>", "hello"},
		{"nested tags", "<div><p>text</p></div>", "text"},
		{"HTML entity unescaped", "&amp;hello&lt;world&gt;", "&hello<world>"},
		{"entity in tag", "<p class=\"x\">text &amp; more</p>", "text & more"},
		{"empty string", "", ""},
		{"tag only", "<br/>", ""},
		{"whitespace trimmed", "  <p>hi</p>  ", "hi"},
		{"mixed content", "Hello <b>world</b> &mdash; bye", "Hello world — bye"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripHTML(tt.input)
			if got != tt.want {
				t.Errorf("stripHTML(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- marshalResult ---

func TestMarshalResult(t *testing.T) {
	result, err := marshalResult(map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}
}

func TestLocalTimeUsesMadridDaylightSaving(t *testing.T) {
	tests := []struct {
		name string
		utc  time.Time
		want string
	}{
		{"winter", time.Date(2026, time.January, 15, 21, 59, 0, 0, time.UTC), "2026-01-15 22:59 CET"},
		{"summer", time.Date(2026, time.July, 15, 21, 59, 0, 0, time.UTC), "2026-07-15 23:59 CEST"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := localTime(tt.utc.Unix()).Format("2006-01-02 15:04 MST")
			if got != tt.want {
				t.Fatalf("localTime() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- Benchmarks ---

func BenchmarkTruncateASCII(b *testing.B) {
	s := "This is a fairly long English string used for benchmarking truncation behaviour."
	for b.Loop() {
		truncate(s, 50)
	}
}

func BenchmarkTruncateUTF8(b *testing.B) {
	s := "これは日本語のテキストで、ベンチマーク用に少し長めに書いてあります。"
	for b.Loop() {
		truncate(s, 10)
	}
}

func BenchmarkStripHTMLSimple(b *testing.B) {
	s := "<p>Hello <b>world</b> &amp; goodbye.</p>"
	for b.Loop() {
		stripHTML(s)
	}
}

func BenchmarkStripHTMLComplex(b *testing.B) {
	s := `<div class="content"><h1>Title &amp; More</h1><p>Some <a href="/link">linked text</a> and <strong>bold</strong>.</p><ul><li>Item 1</li><li>Item 2 &lt;check&gt;</li></ul></div>`
	for b.Loop() {
		stripHTML(s)
	}
}
