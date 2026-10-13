package tools

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata"
)

// unmarshal decodes JSON data into v, returning a descriptive error on failure.
func unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// tagRegexp matches any HTML tag including its attributes.
var tagRegexp = regexp.MustCompile(`<[^>]*>`)

// stripHTML removes all HTML tags and unescapes HTML entities.
// It is safe with multi-byte UTF-8 input.
func stripHTML(s string) string {
	s = tagRegexp.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = strings.TrimSpace(s)
	return s
}

// truncate shortens s to at most max Unicode code points, appending "..." if truncated.
// It is safe with multi-byte UTF-8 strings.
func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}

// marshalResult encodes v as indented JSON and returns it as a string.
var aulesLocation = func() *time.Location {
	location, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		panic(err)
	}
	return location
}()

func localTime(timestamp int64) time.Time {
	return time.Unix(timestamp, 0).In(aulesLocation)
}

func marshalResult(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encoding result: %w", err)
	}
	return string(b), nil
}
