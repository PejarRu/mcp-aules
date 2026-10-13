package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/PejarRu/mcp-aules/internal/api"
)

// --- List Courses Tool ---

type Course struct {
	ID        int    `json:"id"`
	ShortName string `json:"shortname"`
	FullName  string `json:"fullname"`
	Summary   string `json:"summary"`
	StartDate int64  `json:"startdate"`
	EndDate   int64  `json:"enddate"`
	Visible   int    `json:"visible"`
	Progress  *int   `json:"progress,omitempty"`
}

type CourseDisplay struct {
	ID        int    `json:"id"`
	ShortName string `json:"shortname"`
	FullName  string `json:"fullname"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date,omitempty"`
	Active    bool   `json:"active"`
}

type ListCoursesInput struct{}

func HandleListCourses(ctx context.Context, client *api.Client, _ ListCoursesInput) (string, error) {
	if !client.IsAuthenticated() {
		return "", api.ErrNotAuthenticated
	}

	userID := client.GetUserID()
	if userID == 0 {
		return "", fmt.Errorf("user ID not set — please run get_site_info first")
	}

	params := map[string]string{
		"userid": fmt.Sprintf("%d", userID),
	}

	data, err := client.Call(ctx, "core_enrol_get_users_courses", params)
	if err != nil {
		return "", err
	}

	var courses []Course
	if err := json.Unmarshal(data, &courses); err != nil {
		return "", fmt.Errorf("parsing courses: %w", err)
	}

	now := time.Now().Unix()
	var display []CourseDisplay
	for _, c := range courses {
		d := CourseDisplay{
			ID:        c.ID,
			ShortName: c.ShortName,
			FullName:  c.FullName,
			Active:    c.StartDate <= now && (c.EndDate == 0 || c.EndDate >= now),
		}
		if c.StartDate > 0 {
			d.StartDate = time.Unix(c.StartDate, 0).Format("2006-01-02")
		}
		if c.EndDate > 0 {
			d.EndDate = time.Unix(c.EndDate, 0).Format("2006-01-02")
		}
		display = append(display, d)
	}

	result, _ := json.MarshalIndent(map[string]interface{}{
		"total_courses": len(display),
		"courses":       display,
	}, "", "  ")
	return string(result), nil
}

// Helper to get enrolled course IDs
func getEnrolledCourseIDs(ctx context.Context, client *api.Client) ([]int, error) {
	userID := client.GetUserID()
	params := map[string]string{
		"userid": fmt.Sprintf("%d", userID),
	}

	data, err := client.Call(ctx, "core_enrol_get_users_courses", params)
	if err != nil {
		return nil, err
	}

	var courses []Course
	if err := json.Unmarshal(data, &courses); err != nil {
		return nil, err
	}

	ids := make([]int, len(courses))
	for i, c := range courses {
		ids[i] = c.ID
	}
	return ids, nil
}

// --- Get Course Contents Tool ---

type CourseSection struct {
	ID      int            `json:"id"`
	Name    string         `json:"name"`
	Summary string         `json:"summary"`
	Visible int            `json:"visible"`
	Modules []CourseModule `json:"modules"`
}

type CourseModule struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	ModName     string `json:"modname"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
	Visible     int    `json:"visible"`
}

type GetCourseContentsInput struct {
	CourseID int `json:"course_id" jsonschema:"description=The Moodle course ID (get it from list_courses)"`
}

func HandleGetCourseContents(ctx context.Context, client *api.Client, input GetCourseContentsInput) (string, error) {
	if !client.IsAuthenticated() {
		return "", api.ErrNotAuthenticated
	}

	if input.CourseID == 0 {
		return "", fmt.Errorf("course_id is required")
	}

	params := map[string]string{
		"courseid": fmt.Sprintf("%d", input.CourseID),
	}

	data, err := client.Call(ctx, "core_course_get_contents", params)
	if err != nil {
		return "", err
	}

	var sections []CourseSection
	if err := json.Unmarshal(data, &sections); err != nil {
		return "", fmt.Errorf("parsing course contents: %w", err)
	}

	result, _ := json.MarshalIndent(map[string]interface{}{
		"course_id":      input.CourseID,
		"total_sections": len(sections),
		"sections":       sections,
	}, "", "  ")
	return string(result), nil
}

// --- Get Course Details Tool ---

type CourseDetails struct {
	ID           int    `json:"id"`
	ShortName    string `json:"shortname"`
	FullName     string `json:"fullname"`
	Summary      string `json:"summary"`
	CategoryID   int    `json:"categoryid"`
	Format       string `json:"format"`
	StartDate    string `json:"start_date"`
	EndDate      string `json:"end_date,omitempty"`
	NumSections  int    `json:"numsections"`
	EnrolledUser int    `json:"enrolledusercount,omitempty"`
}

type GetCourseDetailsInput struct {
	CourseID int `json:"course_id" jsonschema:"description=The Moodle course ID"`
}

func HandleGetCourseDetails(ctx context.Context, client *api.Client, input GetCourseDetailsInput) (string, error) {
	if !client.IsAuthenticated() {
		return "", api.ErrNotAuthenticated
	}

	if input.CourseID == 0 {
		return "", fmt.Errorf("course_id is required")
	}

	params := map[string]string{
		"options[ids][0]": fmt.Sprintf("%d", input.CourseID),
	}

	data, err := client.Call(ctx, "core_course_get_courses", params)
	if err != nil {
		return "", err
	}

	var courses []json.RawMessage
	if err := json.Unmarshal(data, &courses); err != nil {
		return "", fmt.Errorf("parsing course details: %w", err)
	}

	if len(courses) == 0 {
		return "", fmt.Errorf("course with ID %d not found", input.CourseID)
	}

	// Re-parse the first course with our display struct
	var raw map[string]interface{}
	json.Unmarshal(courses[0], &raw)

	result, _ := json.MarshalIndent(raw, "", "  ")
	return string(result), nil
}
