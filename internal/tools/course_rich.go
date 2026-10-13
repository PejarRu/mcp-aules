package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PejarRu/mcp-aules/internal/api"
)

type ListCourseAnnouncementsInput struct {
	CourseID int `json:"course_id"`
	Limit    int `json:"limit"`
}

func HandleListCourseAnnouncements(ctx context.Context, client *api.Client, input ListCourseAnnouncementsInput) (string, error) {
	if !client.IsAuthenticated() {
		return "", api.ErrNotAuthenticated
	}
	if input.CourseID == 0 {
		return "", fmt.Errorf("course_id is required")
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	data, err := client.Call(ctx, "mod_forum_get_forums_by_courses", map[string]string{"courseids[0]": fmt.Sprint(input.CourseID)})
	if err != nil {
		return "", err
	}
	var forums []map[string]any
	if err := json.Unmarshal(data, &forums); err != nil {
		return "", fmt.Errorf("parsing forums: %w", err)
	}
	var announcements []map[string]any
	for _, forum := range forums {
		if stringValue(forum["type"]) != "news" {
			continue
		}
		discussionData, err := client.Call(ctx, "mod_forum_get_forum_discussions", map[string]string{"forumid": fmt.Sprint(intValue(forum["id"])), "sortorder": "-1", "page": "0", "perpage": fmt.Sprint(limit)})
		if err != nil {
			return "", err
		}
		var response map[string]any
		if err := json.Unmarshal(discussionData, &response); err != nil {
			return "", fmt.Errorf("parsing announcements: %w", err)
		}
		for _, discussion := range mapsValue(response["discussions"]) {
			message := stringValue(discussion["message"])
			discussion["message_html"] = message
			discussion["message_text"] = stripHTML(message)
			discussion["attachments"] = fileMetadata(mapsValue(discussion["attachments"]))
			for _, key := range []string{"created", "modified", "timemodified"} {
				discussion[key+"_rfc3339"] = rfc3339(int64Value(discussion[key]))
			}
			announcements = append(announcements, discussion)
		}
	}
	return marshalResult(map[string]any{"course_id": input.CourseID, "timezone": "Europe/Madrid", "count": len(announcements), "announcements": announcements})
}

type GetCourseLearningContentInput struct {
	CourseID int `json:"course_id"`
}

func HandleGetCourseLearningContent(ctx context.Context, client *api.Client, input GetCourseLearningContentInput) (string, error) {
	if !client.IsAuthenticated() {
		return "", api.ErrNotAuthenticated
	}
	if input.CourseID == 0 {
		return "", fmt.Errorf("course_id is required")
	}
	data, err := client.Call(ctx, "core_course_get_contents", map[string]string{"courseid": fmt.Sprint(input.CourseID)})
	if err != nil {
		return "", err
	}
	var sections []map[string]any
	if err := json.Unmarshal(data, &sections); err != nil {
		return "", fmt.Errorf("parsing learning content: %w", err)
	}
	pages := map[int]map[string]any{}
	var warnings []string
	if pageData, pageErr := client.Call(ctx, "mod_page_get_pages_by_courses", map[string]string{"courseids[0]": fmt.Sprint(input.CourseID)}); pageErr == nil {
		var response map[string]any
		if err := json.Unmarshal(pageData, &response); err != nil {
			warnings = append(warnings, "parsing pages: "+err.Error())
		} else {
			for _, page := range mapsValue(response["pages"]) {
				pages[intValue(page["coursemodule"])] = page
			}
		}
	} else {
		warnings = append(warnings, "getting pages: "+pageErr.Error())
	}
	for _, section := range sections {
		summary := stringValue(section["summary"])
		section["summary_html"] = summary
		section["summary_text"] = stripHTML(summary)
		for _, module := range mapsValue(section["modules"]) {
			if module["modname"] != "page" && module["modname"] != "label" && module["modname"] != "url" {
				continue
			}
			description := stringValue(module["description"])
			module["description_html"] = description
			module["description_text"] = stripHTML(description)
			module["contents"] = fileMetadata(mapsValue(module["contents"]))
			if page := pages[intValue(module["id"])]; page != nil {
				content := stringValue(page["content"])
				module["content_html"] = content
				module["content_text"] = stripHTML(content)
				module["content_files"] = fileMetadata(mapsValue(page["contentfiles"]))
				module["timemodified"] = int64Value(page["timemodified"])
				module["timemodified_rfc3339"] = rfc3339(int64Value(page["timemodified"]))
			}
		}
	}
	return marshalResult(map[string]any{"course_id": input.CourseID, "timezone": "Europe/Madrid", "sections": sections, "warnings": warnings})
}
