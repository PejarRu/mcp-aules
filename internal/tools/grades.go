package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PejarRu/mcp-aules/internal/api"
)

// --- Get Grades Tool ---

type GradeItem struct {
	ID             int     `json:"id"`
	ItemName       string  `json:"itemname"`
	ItemType       string  `json:"itemtype"`
	ItemModule     string  `json:"itemmodule"`
	GradeFormatted string  `json:"gradeformatted"`
	GradeMin       float64 `json:"grademin"`
	GradeMax       float64 `json:"grademax"`
	Feedback       string  `json:"feedback,omitempty"`
	Percentage     string  `json:"percentageformatted,omitempty"`
}

type UserGrade struct {
	CourseID   int         `json:"courseid"`
	UserID     int         `json:"userid"`
	FullName   string      `json:"userfullname"`
	GradeItems []GradeItem `json:"gradeitems"`
}

type GradeReport struct {
	UserGrades []UserGrade `json:"usergrades"`
}

type GetGradesInput struct {
	CourseID int `json:"course_id" jsonschema:"description=The Moodle course ID to get grades for"`
}

func HandleGetGrades(ctx context.Context, client *api.Client, input GetGradesInput) (string, error) {
	if !client.IsAuthenticated() {
		return "", api.ErrNotAuthenticated
	}

	if input.CourseID == 0 {
		return "", fmt.Errorf("course_id is required")
	}

	userID, err := ensureUserID(ctx, client)
	if err != nil {
		return "", err
	}
	params := map[string]string{
		"courseid": fmt.Sprintf("%d", input.CourseID),
		"userid":   fmt.Sprintf("%d", userID),
	}

	data, err := client.Call(ctx, "gradereport_user_get_grade_items", params)
	if err != nil {
		return "", err
	}

	var report GradeReport
	if err := json.Unmarshal(data, &report); err != nil {
		return "", fmt.Errorf("parsing grades: %w", err)
	}

	result, _ := json.MarshalIndent(report, "", "  ")
	return string(result), nil
}

// --- Get Grades Overview Tool ---

type CourseGradeSummary struct {
	CourseID   int    `json:"course_id"`
	CourseName string `json:"course_name"`
	TotalGrade string `json:"total_grade"`
	GradeMax   string `json:"grade_max"`
	ItemCount  int    `json:"graded_items"`
}

type GetGradesOverviewInput struct{}

func HandleGetGradesOverview(ctx context.Context, client *api.Client, _ GetGradesOverviewInput) (string, error) {
	if !client.IsAuthenticated() {
		return "", api.ErrNotAuthenticated
	}

	courseIDs, err := getEnrolledCourseIDs(ctx, client)
	if err != nil {
		return "", fmt.Errorf("getting enrolled courses: %w", err)
	}

	userID := client.GetUserID()
	var summaries []CourseGradeSummary

	for _, cid := range courseIDs {
		params := map[string]string{
			"courseid": fmt.Sprintf("%d", cid),
			"userid":   fmt.Sprintf("%d", userID),
		}

		data, err := client.Call(ctx, "gradereport_user_get_grade_items", params)
		if err != nil {
			continue // Skip courses where we can't access grades
		}

		var report GradeReport
		if err := json.Unmarshal(data, &report); err != nil {
			continue
		}

		if len(report.UserGrades) == 0 {
			continue
		}

		ug := report.UserGrades[0]
		// Find the course total grade item
		var totalGrade, gradeMax string
		gradedCount := 0
		for _, item := range ug.GradeItems {
			if item.ItemType == "course" {
				totalGrade = item.GradeFormatted
				gradeMax = fmt.Sprintf("%.0f", item.GradeMax)
			}
			if item.ItemType == "mod" && item.GradeFormatted != "-" && item.GradeFormatted != "" {
				gradedCount++
			}
		}

		// Get course name
		courseName := fmt.Sprintf("Course %d", cid)
		cParams := map[string]string{
			"options[ids][0]": fmt.Sprintf("%d", cid),
		}
		if cData, err := client.Call(ctx, "core_course_get_courses", cParams); err == nil {
			var courses []struct {
				FullName string `json:"fullname"`
			}
			if json.Unmarshal(cData, &courses) == nil && len(courses) > 0 {
				courseName = courses[0].FullName
			}
		}

		summaries = append(summaries, CourseGradeSummary{
			CourseID:   cid,
			CourseName: courseName,
			TotalGrade: totalGrade,
			GradeMax:   gradeMax,
			ItemCount:  gradedCount,
		})
	}

	result, _ := json.MarshalIndent(map[string]interface{}{
		"total_courses": len(summaries),
		"grades":        summaries,
	}, "", "  ")
	return string(result), nil
}
