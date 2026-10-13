package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/PejarRu/mcp-aules/internal/api"
)

// --- Get Calendar Events Tool ---

type CalendarEvent struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CourseID    int    `json:"courseid"`
	ModuleName  string `json:"modulename"`
	EventType   string `json:"eventtype"`
	TimeStart   int64  `json:"timestart"`
	Duration    int    `json:"timeduration"`
}

type CalendarEventDisplay struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Course string `json:"course,omitempty"`
	Date   string `json:"date"`
	Time   string `json:"time"`
	Module string `json:"module,omitempty"`
}

type CalendarResponse struct {
	Events []CalendarEvent `json:"events"`
}

type GetCalendarEventsInput struct {
	DaysAhead int `json:"days_ahead,omitempty" jsonschema:"description=Number of days to look ahead (default: 30)"`
}

func HandleGetCalendarEvents(ctx context.Context, client *api.Client, input GetCalendarEventsInput) (string, error) {
	if !client.IsAuthenticated() {
		return "", api.ErrNotAuthenticated
	}

	daysAhead := input.DaysAhead
	if daysAhead <= 0 {
		daysAhead = 30
	}

	courseIDs, err := getEnrolledCourseIDs(ctx, client)
	if err != nil {
		return "", err
	}

	now := time.Now()
	params := map[string]string{
		"options[timestart]": fmt.Sprintf("%d", now.Unix()),
		"options[timeend]":   fmt.Sprintf("%d", now.Add(time.Duration(daysAhead)*24*time.Hour).Unix()),
	}

	// Add course IDs
	for i, cid := range courseIDs {
		params[fmt.Sprintf("events[courseids][%d]", i)] = fmt.Sprintf("%d", cid)
	}

	data, err := client.Call(ctx, "core_calendar_get_calendar_events", params)
	if err != nil {
		return "", err
	}

	var resp CalendarResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("parsing calendar events: %w", err)
	}

	var display []CalendarEventDisplay
	for _, e := range resp.Events {
		t := time.Unix(e.TimeStart, 0)
		display = append(display, CalendarEventDisplay{
			ID:     e.ID,
			Name:   e.Name,
			Type:   e.EventType,
			Date:   t.Format("2006-01-02"),
			Time:   t.Format("15:04"),
			Module: e.ModuleName,
		})
	}

	// Sort by date
	sort.Slice(display, func(i, j int) bool {
		return display[i].Date < display[j].Date
	})

	result, _ := json.MarshalIndent(map[string]interface{}{
		"days_ahead":   daysAhead,
		"total_events": len(display),
		"events":       display,
	}, "", "  ")
	return string(result), nil
}

// --- Get Upcoming Deadlines Tool ---

type Deadline struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	CourseName string `json:"course"`
	DueDate    string `json:"due_date"`
	DueTime    string `json:"due_time"`
	DaysLeft   int    `json:"days_left"`
	Urgent     bool   `json:"urgent"`
}

type GetUpcomingDeadlinesInput struct {
	DaysAhead int `json:"days_ahead,omitempty" jsonschema:"description=Number of days to look ahead (default: 14)"`
}

func HandleGetUpcomingDeadlines(ctx context.Context, client *api.Client, input GetUpcomingDeadlinesInput) (string, error) {
	if !client.IsAuthenticated() {
		return "", api.ErrNotAuthenticated
	}

	daysAhead := input.DaysAhead
	if daysAhead <= 0 {
		daysAhead = 14
	}

	courseIDs, err := getEnrolledCourseIDs(ctx, client)
	if err != nil {
		return "", err
	}

	now := time.Now()
	cutoff := now.Add(time.Duration(daysAhead) * 24 * time.Hour)

	// Build a course ID → name map
	courseNames := make(map[int]string)
	for _, cid := range courseIDs {
		cParams := map[string]string{
			"options[ids][0]": fmt.Sprintf("%d", cid),
		}
		if cData, err := client.Call(ctx, "core_course_get_courses", cParams); err == nil {
			var courses []struct {
				FullName string `json:"fullname"`
			}
			if json.Unmarshal(cData, &courses) == nil && len(courses) > 0 {
				courseNames[cid] = courses[0].FullName
			}
		}
	}

	var deadlines []Deadline

	// Get assignment deadlines
	for _, cid := range courseIDs {
		params := map[string]string{
			"courseids[0]": fmt.Sprintf("%d", cid),
		}
		data, err := client.Call(ctx, "mod_assign_get_assignments", params)
		if err != nil {
			continue
		}

		var resp AssignmentResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			continue
		}

		for _, course := range resp.Courses {
			for _, a := range course.Assignments {
				if a.DueDate == 0 {
					continue
				}
				due := time.Unix(a.DueDate, 0)
				if due.After(now) && due.Before(cutoff) {
					daysLeft := int(time.Until(due).Hours() / 24)
					deadlines = append(deadlines, Deadline{
						Name:       a.Name,
						Type:       "assignment",
						CourseName: courseNames[cid],
						DueDate:    due.Format("2006-01-02"),
						DueTime:    due.Format("15:04"),
						DaysLeft:   daysLeft,
						Urgent:     daysLeft <= 3,
					})
				}
			}
		}
	}

	// Get calendar event deadlines
	calParams := map[string]string{
		"options[timestart]": fmt.Sprintf("%d", now.Unix()),
		"options[timeend]":   fmt.Sprintf("%d", cutoff.Unix()),
	}
	for i, cid := range courseIDs {
		calParams[fmt.Sprintf("events[courseids][%d]", i)] = fmt.Sprintf("%d", cid)
	}
	if calData, err := client.Call(ctx, "core_calendar_get_calendar_events", calParams); err == nil {
		var calResp CalendarResponse
		if json.Unmarshal(calData, &calResp) == nil {
			for _, e := range calResp.Events {
				if e.EventType == "due" || e.EventType == "close" {
					due := time.Unix(e.TimeStart, 0)
					daysLeft := int(time.Until(due).Hours() / 24)
					deadlines = append(deadlines, Deadline{
						Name:       e.Name,
						Type:       e.EventType,
						CourseName: courseNames[e.CourseID],
						DueDate:    due.Format("2006-01-02"),
						DueTime:    due.Format("15:04"),
						DaysLeft:   daysLeft,
						Urgent:     daysLeft <= 3,
					})
				}
			}
		}
	}

	// Sort by due date (most urgent first)
	sort.Slice(deadlines, func(i, j int) bool {
		return deadlines[i].DaysLeft < deadlines[j].DaysLeft
	})

	// Deduplicate by name+date
	seen := make(map[string]bool)
	var unique []Deadline
	for _, d := range deadlines {
		key := d.Name + d.DueDate
		if !seen[key] {
			seen[key] = true
			unique = append(unique, d)
		}
	}

	result, _ := json.MarshalIndent(map[string]interface{}{
		"days_ahead":      daysAhead,
		"total_deadlines": len(unique),
		"deadlines":       unique,
	}, "", "  ")
	return string(result), nil
}
