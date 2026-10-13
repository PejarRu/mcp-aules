package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PejarRu/mcp-aules/internal/api"
)

type GetCourseGradebookInput struct {
	CourseID int `json:"course_id"`
}

func HandleGetCourseGradebook(ctx context.Context, client *api.Client, input GetCourseGradebookInput) (string, error) {
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
	data, err := client.Call(ctx, "gradereport_user_get_grade_items", map[string]string{"courseid": fmt.Sprint(input.CourseID), "userid": fmt.Sprint(userID)})
	if err != nil {
		return "", err
	}
	var response map[string]any
	if err := json.Unmarshal(data, &response); err != nil {
		return "", fmt.Errorf("parsing gradebook: %w", err)
	}
	var courseTotal map[string]any
	for _, userGrade := range mapsValue(response["usergrades"]) {
		for _, item := range mapsValue(userGrade["gradeitems"]) {
			item["feedback_html"] = stringValue(item["feedback"])
			item["feedback_text"] = stripHTML(stringValue(item["feedback"]))
			for _, key := range []string{"gradedategraded", "gradedatesubmitted"} {
				item[key+"_rfc3339"] = rfc3339(int64Value(item[key]))
			}
			if stringValue(item["itemtype"]) == "course" {
				courseTotal = item
			}
		}
	}
	response["course_id"] = input.CourseID
	response["timezone"] = "Europe/Madrid"
	response["course_total"] = courseTotal
	return marshalResult(response)
}
