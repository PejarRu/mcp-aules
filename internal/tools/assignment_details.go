package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/PejarRu/mcp-aules/internal/api"
)

type GetAssignmentDetailsInput struct {
	AssignmentID int `json:"assignment_id"`
}
type GetAssignmentStatusInput struct {
	AssignmentID int `json:"assignment_id"`
}

func findAssignment(ctx context.Context, client *api.Client, assignmentID int) (map[string]any, int, error) {
	if assignmentID == 0 {
		return nil, 0, fmt.Errorf("assignment_id is required")
	}
	courseIDs, err := getEnrolledCourseIDs(ctx, client)
	if err != nil {
		return nil, 0, err
	}
	params := map[string]string{}
	for i, id := range courseIDs {
		params[fmt.Sprintf("courseids[%d]", i)] = fmt.Sprint(id)
	}
	data, err := client.Call(ctx, "mod_assign_get_assignments", params)
	if err != nil {
		return nil, 0, err
	}
	var response map[string]any
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, 0, fmt.Errorf("parsing assignments: %w", err)
	}
	for _, course := range mapsValue(response["courses"]) {
		for _, assignment := range mapsValue(course["assignments"]) {
			if intValue(assignment["id"]) == assignmentID {
				return assignment, intValue(course["id"]), nil
			}
		}
	}
	return nil, 0, fmt.Errorf("assignment %d not found in enrolled courses", assignmentID)
}

func assignmentFiles(assignment map[string]any) []FileMetadata {
	var files []FileMetadata
	for _, key := range []string{"introfiles", "introattachments"} {
		files = append(files, fileMetadata(mapsValue(assignment[key]))...)
	}
	if data, ok := assignment["assignmentdata"].(map[string]any); ok {
		files = append(files, fileMetadata(mapsValue(data["attachments"]))...)
	}
	return files
}

func HandleGetAssignmentDetails(ctx context.Context, client *api.Client, input GetAssignmentDetailsInput) (string, error) {
	if !client.IsAuthenticated() {
		return "", api.ErrNotAuthenticated
	}
	a, courseID, err := findAssignment(ctx, client, input.AssignmentID)
	if err != nil {
		return "", err
	}
	intro := stringValue(a["intro"])
	attachments := assignmentFiles(a)
	if statusData, statusErr := client.Call(ctx, "mod_assign_get_submission_status", map[string]string{"assignid": fmt.Sprint(input.AssignmentID)}); statusErr == nil {
		var status map[string]any
		if json.Unmarshal(statusData, &status) == nil {
			if assignmentData, ok := status["assignmentdata"].(map[string]any); ok {
				attachments = uniqueFiles(append(attachments, fileMetadata(mapsValue(assignmentData["attachments"]))...))
			}
		}
	}
	out := map[string]any{"id": intValue(a["id"]), "course_id": courseID, "timezone": "Europe/Madrid", "cmid": intValue(a["cmid"]), "name": stringValue(a["name"]), "description_html": intro, "description_text": stripHTML(intro), "introformat": intValue(a["introformat"]), "attachments": attachments}
	for _, key := range []string{"allowsubmissionsfromdate", "duedate", "cutoffdate", "gradingduedate", "timemodified"} {
		ts := int64Value(a[key])
		out[key] = ts
		out[key+"_rfc3339"] = rfc3339(ts)
	}
	for _, key := range []string{"grade", "completionsubmit", "requiresubmissionstatement", "teamsubmission", "requireallteammemberssubmit", "blindmarking", "hidegrader", "attemptreopenmethod", "maxattempts", "markingworkflow", "markingallocation", "sendnotifications", "sendlatenotifications"} {
		if value, ok := a[key]; ok {
			out[key] = value
		}
	}
	return marshalResult(out)
}

func HandleGetAssignmentStatus(ctx context.Context, client *api.Client, input GetAssignmentStatusInput) (string, error) {
	if !client.IsAuthenticated() {
		return "", api.ErrNotAuthenticated
	}
	a, courseID, err := findAssignment(ctx, client, input.AssignmentID)
	if err != nil {
		return "", err
	}
	data, err := client.Call(ctx, "mod_assign_get_submission_status", map[string]string{"assignid": fmt.Sprint(input.AssignmentID)})
	if err != nil {
		return "", err
	}
	var response map[string]any
	if err := json.Unmarshal(data, &response); err != nil {
		return "", fmt.Errorf("parsing submission status: %w", err)
	}
	lastAttempt, _ := response["lastattempt"].(map[string]any)
	submission, _ := lastAttempt["submission"].(map[string]any)
	effectiveDue := int64Value(a["duedate"])
	if extension := int64Value(lastAttempt["extensionduedate"]); extension > 0 {
		effectiveDue = extension
	}
	status, submittedAt := deriveAssignmentState(submission, lastAttempt, effectiveDue)
	attemptNumber := intValue(submission["attemptnumber"])
	if status == "not_submitted" && attemptNumber > 0 {
		status = "reopened"
	}
	plugins := mapsValue(submission["plugins"])
	var files []FileMetadata
	var onlineHTML string
	for _, plugin := range plugins {
		for _, area := range mapsValue(plugin["fileareas"]) {
			files = append(files, fileMetadata(mapsValue(area["files"]))...)
		}
		for _, field := range mapsValue(plugin["editorfields"]) {
			if stringValue(field["name"]) == "onlinetext" || onlineHTML == "" {
				onlineHTML = stringValue(field["text"])
			}
		}
	}
	feedback, _ := response["feedback"].(map[string]any)
	grading := "not_graded"
	if boolValue(lastAttempt["graded"]) || stringValue(lastAttempt["gradingstatus"]) == "graded" || len(feedback) > 0 {
		grading = "graded"
	} else if status == "submitted" || status == "submitted_late" {
		grading = "pending"
	}
	duePassed := effectiveDue > 0 && localTime(effectiveDue).Before(nowMadrid())
	out := map[string]any{"assignment_id": input.AssignmentID, "course_id": courseID, "timezone": "Europe/Madrid", "submission_status": status, "grading_status": grading, "submitted_at": rfc3339(submittedAt), "submitted_at_timestamp": submittedAt, "attempt_number": attemptNumber, "due_timestamp": int64Value(a["duedate"]), "due_at": rfc3339(int64Value(a["duedate"])), "effective_due_timestamp": effectiveDue, "effective_due_at": rfc3339(effectiveDue), "due_passed": duePassed, "is_late": submittedAt > 0 && effectiveDue > 0 && submittedAt > effectiveDue, "is_overdue": submittedAt == 0 && duePassed, "canedit": boolValue(lastAttempt["canedit"]), "cansubmit": boolValue(lastAttempt["cansubmit"]), "locked": boolValue(lastAttempt["locked"]), "submissionsenabled": boolValue(lastAttempt["submissionsenabled"]), "reopen_policy": stringValue(a["attemptreopenmethod"]), "maxattempts": intValue(a["maxattempts"]), "submitted_files": uniqueFiles(files), "online_text_html": onlineHTML, "online_text": stripHTML(onlineHTML)}
	if effectiveDue > 0 {
		out["is_overdue"] = submittedAt == 0 && localTime(effectiveDue).Before(nowMadrid())
	}
	if len(feedback) > 0 {
		out["grade_feedback"] = feedback
	}
	return marshalResult(out)
}

func nowMadrid() time.Time { return time.Now().In(aulesLocation) }

func deriveAssignmentState(submission, response map[string]any, due int64) (string, int64) {
	status := strings.ToLower(stringValue(submission["status"]))
	timeModified := int64Value(submission["timemodified"])
	if status == "reopened" {
		return "reopened", timeModified
	}
	switch status {
	case "draft":
		return "draft", timeModified
	case "submitted":
		if due > 0 && timeModified > due {
			return "submitted_late", timeModified
		}
		return "submitted", timeModified
	case "reopened":
		return "reopened", timeModified
	default:
		return "not_submitted", 0
	}
}
