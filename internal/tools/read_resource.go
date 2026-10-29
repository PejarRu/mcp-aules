package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	pdf "github.com/ledongthuc/pdf"

	"github.com/PejarRu/mcp-aules/internal/api"
)

type ReadResourceInput struct {
	CourseID int `json:"course_id" jsonschema:"description=The Moodle course ID containing the resource"`
	ModuleID int `json:"module_id" jsonschema:"description=The module ID of the resource (get from list_resources)"`
	MaxChars int `json:"max_chars,omitempty" jsonschema:"description=Maximum characters of extracted text to return (default 20000)"`
}

func HandleReadResource(ctx context.Context, client *api.Client, input ReadResourceInput) (string, error) {
	if !client.IsAuthenticated() {
		return "", api.ErrNotAuthenticated
	}
	if input.CourseID == 0 {
		return "", fmt.Errorf("course_id is required")
	}
	if input.ModuleID == 0 {
		return "", fmt.Errorf("module_id is required")
	}

	maxChars := input.MaxChars
	if maxChars <= 0 {
		maxChars = 20000
	}
	if maxChars > 100000 {
		maxChars = 100000
	}

	sections, err := getCourseContentsRaw(ctx, client, input.CourseID)
	if err != nil {
		return "", err
	}

	var targetFile *FileContent
	var moduleName string
	for _, sec := range sections {
		for _, mod := range sec.Modules {
			if mod.ID == input.ModuleID {
				moduleName = mod.Name
				for _, f := range mod.Contents {
					if f.Type == "file" {
						fc := f
						targetFile = &fc
						break
					}
				}
			}
		}
	}
	if targetFile == nil {
		return "", fmt.Errorf("no downloadable file found for module_id %d in course %d", input.ModuleID, input.CourseID)
	}

	// Cap download size to avoid huge files in memory.
	if targetFile.FileSize > 25*1024*1024 {
		return "", fmt.Errorf("file too large to read inline (%d bytes)", targetFile.FileSize)
	}

	downloadURL := targetFile.FileURL
	if strings.Contains(downloadURL, "?") {
		downloadURL += "&token=" + client.GetToken()
	} else {
		downloadURL += "?token=" + client.GetToken()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating download request: %w", err)
	}
	httpClient := &http.Client{Timeout: 120 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("downloading file: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 25*1024*1024+1))
	if err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}

	text, format, err := extractText(targetFile.Filename, targetFile.MimeType, data)
	if err != nil {
		return "", err
	}

	if len([]rune(text)) > maxChars {
		runes := []rune(text)
		text = string(runes[:maxChars]) + "\n...[truncated]"
	}

	res, _ := json.MarshalIndent(map[string]interface{}{
		"module_name": moduleName,
		"filename":    targetFile.Filename,
		"mime_type":   targetFile.MimeType,
		"format":      format,
		"char_count":  len([]rune(text)),
		"text":        text,
	}, "", "  ")
	return string(res), nil
}

func extractText(filename, mimeType string, data []byte) (string, string, error) {
	mime := strings.ToLower(mimeType)
	name := strings.ToLower(filename)

	if strings.Contains(mime, "pdf") || strings.HasSuffix(name, ".pdf") {
		r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return "", "", fmt.Errorf("opening pdf: %w", err)
		}
		tr, err := r.GetPlainText()
		if err != nil {
			return "", "", fmt.Errorf("extracting pdf text: %w", err)
		}
		buf, err := io.ReadAll(tr)
		if err != nil {
			return "", "", fmt.Errorf("reading pdf text: %w", err)
		}
		return string(buf), "pdf", nil
	}

	if strings.Contains(mime, "text/") || strings.HasSuffix(name, ".txt") || strings.HasSuffix(name, ".csv") || strings.HasSuffix(name, ".md") {
		return string(data), "text", nil
	}

	if strings.Contains(mime, "html") || strings.HasSuffix(name, ".html") || strings.HasSuffix(name, ".htm") {
		// Strip tags for readable text.
		s := string(data)
		// remove scripts/styles
		reScript := removeByRegexp(s, `(?is)<script.*?</script>`)
		reStyle := removeByRegexp(reScript, `(?is)<style.*?</style>`)
		text := stripHTML(reStyle)
		return text, "html", nil
	}

	// docx: zip with word/document.xml
	if strings.Contains(mime, "wordprocessingml") || strings.HasSuffix(name, ".docx") {
		text, err := extractDocx(data)
		if err != nil {
			return "", "", err
		}
		return text, "docx", nil
	}

	return "", "", fmt.Errorf("unsupported file type for text extraction: %s (%s)", mimeType, filename)
}

func removeByRegexp(s, pattern string) string {
	re := regexp.MustCompile(pattern)
	return re.ReplaceAllString(s, "")
}

func extractDocx(data []byte) (string, error) {
	// Minimal: find word/document.xml in zip and strip tags.
	r := bytes.NewReader(data)
	// Use archive/zip
	zr, err := zip.NewReader(r, int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("opening docx: %w", err)
	}
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			defer rc.Close()
			b, err := io.ReadAll(rc)
			if err != nil {
				return "", err
			}
			text := stripHTML(string(b))
			// collapse whitespace
			text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")
			return strings.TrimSpace(text), nil
		}
	}
	return "", fmt.Errorf("word/document.xml not found in docx")
}
