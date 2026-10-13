package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PejarRu/mcp-aules/internal/api"
)

// newTestClient creates an API client pointed at a mock HTTP server.
func newTestClient(t *testing.T, handler http.Handler) (*api.Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	c := api.NewClient()
	c.SetSession(srv.URL, "testtoken")
	c.SetUserID(1)
	return c, srv
}

func TestHandleListCourses_UnauthenticatedError(t *testing.T) {
	c := api.NewClient()
	_, err := HandleListCourses(context.Background(), c, ListCoursesInput{})
	if err == nil {
		t.Fatal("expected error when not authenticated")
	}
}

func TestHandleListCourses_Success(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": 1, "shortname": "CS101", "fullname": "Intro to CS", "startdate": 1700000000, "enddate": 0},
		})
	})
	c, srv := newTestClient(t, handler)
	defer srv.Close()

	result, err := HandleListCourses(context.Background(), c, ListCoursesInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if out["total_courses"].(float64) != 1 {
		t.Errorf("total_courses = %v, want 1", out["total_courses"])
	}
}

func TestHandleGetAssignments_MissingCourseID(t *testing.T) {
	c, srv := newTestClient(t, http.NotFoundHandler())
	defer srv.Close()

	_, err := HandleGetAssignments(context.Background(), c, GetAssignmentsInput{CourseID: 0})
	if err == nil {
		t.Fatal("expected error for missing course_id")
	}
}

func TestHandleGetAssignments_PreservesContentAndTeacherFiles(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assignment := map[string]any{
			"id": 12, "cmid": 34, "name": "Trabajo", "intro": "<p>Descripción <b>completa</b></p>", "introformat": 1,
			"duedate": 1700000000, "introfiles": []any{map[string]any{"filename": "guia.pdf", "filepath": "/", "mimetype": "application/pdf", "filesize": 42, "timemodified": 1700000100, "fileurl": "https://example.test/guia.pdf?token=secret&x=1"}},
		}
		json.NewEncoder(w).Encode(map[string]any{"courses": []any{map[string]any{"id": 7, "assignments": []any{assignment}}}})
	})
	c, srv := newTestClient(t, handler)
	defer srv.Close()
	result, err := HandleGetAssignments(context.Background(), c, GetAssignmentsInput{CourseID: 7})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatal(err)
	}
	assignment := out["assignments"].([]any)[0].(map[string]any)
	if assignment["description_html"] != "<p>Descripción <b>completa</b></p>" || assignment["description_text"] != "Descripción completa" {
		t.Fatalf("unexpected descriptions: %#v", assignment)
	}
	file := assignment["teacher_attachments"].([]any)[0].(map[string]any)
	if file["fileurl"] != "https://example.test/guia.pdf?x=1" {
		t.Fatalf("token not removed: %v", file["fileurl"])
	}
	if out["timezone"] != "Europe/Madrid" {
		t.Fatalf("timezone = %v", out["timezone"])
	}
}

func TestDeriveAssignmentState(t *testing.T) {
	tests := []struct {
		status   string
		modified int64
		due      int64
		want     string
	}{
		{"draft", 20, 10, "draft"},
		{"submitted", 10, 20, "submitted"},
		{"submitted", 30, 20, "submitted_late"},
		{"reopened", 30, 20, "reopened"},
		{"new", 0, 20, "not_submitted"},
	}
	for _, test := range tests {
		got, _ := deriveAssignmentState(map[string]any{"status": test.status, "timemodified": float64(test.modified)}, map[string]any{}, test.due)
		if got != test.want {
			t.Errorf("status %q = %q, want %q", test.status, got, test.want)
		}
	}
}

func TestHandleGetAssignmentStatus_SubmittedLate(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("wsfunction") {
		case "core_enrol_get_users_courses":
			json.NewEncoder(w).Encode([]map[string]any{{"id": 7}})
		case "mod_assign_get_assignments":
			json.NewEncoder(w).Encode(map[string]any{"courses": []any{map[string]any{"id": 7, "assignments": []any{map[string]any{"id": 12, "duedate": 100, "attemptreopenmethod": "manual", "maxattempts": 3}}}}})
		case "mod_assign_get_submission_status":
			json.NewEncoder(w).Encode(map[string]any{"lastattempt": map[string]any{"graded": false, "gradingstatus": "notgraded", "locked": true, "canedit": false, "cansubmit": false, "submissionsenabled": true, "submission": map[string]any{"status": "submitted", "attemptnumber": 2, "timemodified": 120, "plugins": []any{}}}})
		default:
			t.Fatalf("unexpected function %q", r.URL.Query().Get("wsfunction"))
		}
	})
	client, server := newTestClient(t, handler)
	defer server.Close()
	result, err := HandleGetAssignmentStatus(context.Background(), client, GetAssignmentStatusInput{AssignmentID: 12})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatal(err)
	}
	if out["submission_status"] != "submitted_late" || out["grading_status"] != "pending" || out["is_late"] != true || out["is_overdue"] != false || out["locked"] != true || out["attempt_number"] != float64(2) {
		t.Fatalf("unexpected status: %#v", out)
	}
}

func TestHandleGetGrades_MissingCourseID(t *testing.T) {
	c, srv := newTestClient(t, http.NotFoundHandler())
	defer srv.Close()

	_, err := HandleGetGrades(context.Background(), c, GetGradesInput{CourseID: 0})
	if err == nil {
		t.Fatal("expected error for missing course_id")
	}
}

func TestHandleGetCourseContents_MissingCourseID(t *testing.T) {
	c, srv := newTestClient(t, http.NotFoundHandler())
	defer srv.Close()

	_, err := HandleGetCourseContents(context.Background(), c, GetCourseContentsInput{CourseID: 0})
	if err == nil {
		t.Fatal("expected error for missing course_id")
	}
}

func TestHandleGetCourseDetails_MissingCourseID(t *testing.T) {
	c, srv := newTestClient(t, http.NotFoundHandler())
	defer srv.Close()

	_, err := HandleGetCourseDetails(context.Background(), c, GetCourseDetailsInput{CourseID: 0})
	if err == nil {
		t.Fatal("expected error for missing course_id")
	}
}

func TestHandleSubmitAssignment_MissingFields(t *testing.T) {
	c, srv := newTestClient(t, http.NotFoundHandler())
	defer srv.Close()

	tests := []struct {
		name  string
		input SubmitAssignmentInput
	}{
		{"missing assignment_id", SubmitAssignmentInput{Text: "hello"}},
		{"missing text", SubmitAssignmentInput{AssignmentID: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := HandleSubmitAssignment(context.Background(), c, tt.input)
			if err == nil {
				t.Fatal("expected error for missing required field")
			}
		})
	}
}

func TestHandleLogin_MissingFields(t *testing.T) {
	c := api.NewClient()
	tests := []struct {
		name  string
		input LoginInput
	}{
		{"missing all", LoginInput{}},
		{"missing username", LoginInput{MoodleURL: "https://m.example.com", Password: "p"}},
		{"missing password", LoginInput{MoodleURL: "https://m.example.com", Username: "u"}},
		{"missing url", LoginInput{Username: "u", Password: "p"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := HandleLogin(context.Background(), c, tt.input)
			if err == nil {
				t.Fatal("expected error for missing required field")
			}
		})
	}
}

func TestHandleGetNotifications_DefaultsApplied(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify default limit and read type are sent
		q := r.URL.Query()
		if q.Get("limitnum") == "0" {
			http.Error(w, "bad limit", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"messages": []any{}})
	})
	c, srv := newTestClient(t, handler)
	defer srv.Close()

	// Zero limit should default to 20
	result, err := HandleGetNotifications(context.Background(), c, GetNotificationsInput{Limit: 0, UnreadOnly: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}
}

// BenchmarkHandleListCourses measures end-to-end handler performance with a mock server.
func BenchmarkHandleListCourses(b *testing.B) {
	courses := make([]map[string]any, 10)
	for i := range courses {
		courses[i] = map[string]any{
			"id": i + 1, "shortname": "CS", "fullname": "Course", "startdate": 1700000000, "enddate": 0,
		}
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(courses)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c := api.NewClient()
	c.SetSession(srv.URL, "tok")
	c.SetUserID(1)
	ctx := context.Background()

	b.ResetTimer()
	for b.Loop() {
		HandleListCourses(ctx, c, ListCoursesInput{})
	}
}
