package tools

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

type FileMetadata struct {
	FileName     string `json:"filename"`
	FilePath     string `json:"filepath,omitempty"`
	MimeType     string `json:"mimetype,omitempty"`
	FileSize     int64  `json:"filesize,omitempty"`
	TimeModified int64  `json:"timemodified,omitempty"`
	ModifiedAt   string `json:"modified_at,omitempty"`
	FileURL      string `json:"fileurl,omitempty"`
}

func rfc3339(timestamp int64) string {
	if timestamp <= 0 {
		return ""
	}
	return localTime(timestamp).Format(time.RFC3339)
}

func uniqueFiles(files []FileMetadata) []FileMetadata {
	seen := map[string]bool{}
	out := make([]FileMetadata, 0, len(files))
	for _, file := range files {
		key := file.FilePath + "\x00" + file.FileName + "\x00" + file.FileURL
		if !seen[key] {
			seen[key] = true
			out = append(out, file)
		}
	}
	return out
}

func fileMetadata(files []map[string]any) []FileMetadata {
	out := make([]FileMetadata, 0, len(files))
	for _, file := range files {
		timestamp := int64Value(file["timemodified"])
		out = append(out, FileMetadata{
			FileName: stringValue(file["filename"]), FilePath: stringValue(file["filepath"]),
			MimeType: stringValue(file["mimetype"]), FileSize: int64Value(file["filesize"]),
			TimeModified: timestamp, ModifiedAt: rfc3339(timestamp), FileURL: removeToken(stringValue(file["fileurl"])),
		})
	}
	return out
}

func removeToken(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	for key := range q {
		if strings.EqualFold(key, "token") || strings.EqualFold(key, "wstoken") {
			q.Del(key)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func intValue(value any) int { return int(int64Value(value)) }

func int64Value(value any) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case int:
		return int64(v)
	case int64:
		return v
	case string:
		var result int64
		fmt.Sscan(v, &result)
		return result
	default:
		return 0
	}
}

func floatValue(value any) float64 {
	if v, ok := value.(float64); ok {
		return v
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if v, ok := value.(string); ok {
		return v
	}
	return fmt.Sprint(value)
}

func boolValue(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case float64:
		return v != 0
	case string:
		return v == "1" || strings.EqualFold(v, "true")
	default:
		return false
	}
}

func mapsValue(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if value, ok := item.(map[string]any); ok {
			out = append(out, value)
		}
	}
	return out
}
