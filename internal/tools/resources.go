package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PejarRu/mcp-aules/internal/api"
)

// --- Resource types ---

type FileContent struct {
	Type     string `json:"type"`
	Filename string `json:"filename"`
	FileURL  string `json:"fileurl"`
	FileSize int64  `json:"filesize"`
	MimeType string `json:"mimetype"`
}

type ResourceModule struct {
	ID       int           `json:"id"`
	Name     string        `json:"name"`
	ModName  string        `json:"modname"`
	Contents []FileContent `json:"contents"`
}

type ResourceSection struct {
	Name    string           `json:"name"`
	Modules []ResourceModule `json:"modules"`
}

type ResourceDisplay struct {
	ModuleID int    `json:"module_id"`
	Name     string `json:"name"`
	Filename string `json:"filename"`
	SizeMB   string `json:"size_mb"`
	MimeType string `json:"mime_type"`
	Section  string `json:"section"`
}

// --- List Resources Tool ---

type ListResourcesInput struct {
	CourseID int    `json:"course_id" jsonschema:"description=The Moodle course ID to list downloadable files for"`
	MimeType string `json:"mime_type,omitempty" jsonschema:"description=Filter by MIME type e.g. application/pdf (optional)"`
}

func HandleListResources(ctx context.Context, client *api.Client, input ListResourcesInput) (string, error) {
	if !client.IsAuthenticated() {
		return "", api.ErrNotAuthenticated
	}
	if input.CourseID == 0 {
		return "", fmt.Errorf("course_id is required")
	}

	sections, err := getCourseContentsRaw(ctx, client, input.CourseID)
	if err != nil {
		return "", err
	}

	var resources []ResourceDisplay
	for _, sec := range sections {
		for _, mod := range sec.Modules {
			for _, f := range mod.Contents {
				if f.Type != "file" {
					continue
				}
				if input.MimeType != "" && !strings.Contains(f.MimeType, input.MimeType) {
					continue
				}
				resources = append(resources, ResourceDisplay{
					ModuleID: mod.ID,
					Name:     mod.Name,
					Filename: f.Filename,
					SizeMB:   fmt.Sprintf("%.1f MB", float64(f.FileSize)/1024/1024),
					MimeType: f.MimeType,
					Section:  sec.Name,
				})
			}
		}
	}

	result, _ := json.MarshalIndent(map[string]interface{}{
		"course_id":   input.CourseID,
		"total_files": len(resources),
		"resources":   resources,
	}, "", "  ")
	return string(result), nil
}

// --- Download Resource Tool ---

type DownloadResourceInput struct {
	CourseID int    `json:"course_id" jsonschema:"description=The Moodle course ID containing the resource"`
	ModuleID int    `json:"module_id" jsonschema:"description=The module ID of the resource to download (get from list_resources)"`
	SaveDir  string `json:"save_dir,omitempty" jsonschema:"description=Directory to save the file (default: ~/Downloads)"`
}

func HandleDownloadResource(ctx context.Context, client *api.Client, input DownloadResourceInput) (string, error) {
	if !client.IsAuthenticated() {
		return "", api.ErrNotAuthenticated
	}
	if input.CourseID == 0 {
		return "", fmt.Errorf("course_id is required")
	}
	if input.ModuleID == 0 {
		return "", fmt.Errorf("module_id is required")
	}

	// Resolve save directory
	saveDir := input.SaveDir
	if saveDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			saveDir = "."
		} else {
			saveDir = filepath.Join(home, "Downloads")
		}
	}
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		return "", fmt.Errorf("creating save directory: %w", err)
	}

	// Find the file in course contents
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

	// Build authenticated download URL
	downloadURL := targetFile.FileURL
	if strings.Contains(downloadURL, "?") {
		downloadURL += "&token=" + client.GetToken()
	} else {
		downloadURL += "?token=" + client.GetToken()
	}

	// Download the file
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

	savePath := filepath.Join(saveDir, targetFile.Filename)
	out, err := os.Create(savePath)
	if err != nil {
		return "", fmt.Errorf("creating file: %w", err)
	}
	defer out.Close()

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		return "", fmt.Errorf("saving file: %w", err)
	}

	result, _ := json.MarshalIndent(map[string]interface{}{
		"success":     true,
		"module_name": moduleName,
		"filename":    targetFile.Filename,
		"saved_to":    savePath,
		"size_mb":     fmt.Sprintf("%.1f MB", float64(written)/1024/1024),
		"mime_type":   targetFile.MimeType,
	}, "", "  ")
	return string(result), nil
}

// --- Internal helpers ---

func getCourseContentsRaw(ctx context.Context, client *api.Client, courseID int) ([]ResourceSection, error) {
	params := map[string]string{
		"courseid": fmt.Sprintf("%d", courseID),
	}
	data, err := client.Call(ctx, "core_course_get_contents", params)
	if err != nil {
		return nil, fmt.Errorf("getting course contents: %w", err)
	}

	var raw []struct {
		Name    string `json:"name"`
		Modules []struct {
			ID       int    `json:"id"`
			Name     string `json:"name"`
			ModName  string `json:"modname"`
			Contents []struct {
				Type     string `json:"type"`
				Filename string `json:"filename"`
				FileURL  string `json:"fileurl"`
				FileSize int64  `json:"filesize"`
				MimeType string `json:"mimetype"`
			} `json:"contents"`
		} `json:"modules"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing course contents: %w", err)
	}

	var sections []ResourceSection
	for _, s := range raw {
		sec := ResourceSection{Name: s.Name}
		for _, m := range s.Modules {
			mod := ResourceModule{ID: m.ID, Name: m.Name, ModName: m.ModName}
			for _, c := range m.Contents {
				mod.Contents = append(mod.Contents, FileContent{
					Type:     c.Type,
					Filename: c.Filename,
					FileURL:  c.FileURL,
					FileSize: c.FileSize,
					MimeType: c.MimeType,
				})
			}
			sec.Modules = append(sec.Modules, mod)
		}
		sections = append(sections, sec)
	}
	return sections, nil
}
