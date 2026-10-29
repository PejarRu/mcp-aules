package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/PejarRu/mcp-aules/internal/api"
)

type GetRecentChangesInput struct {
	CourseID int   `json:"course_id,omitempty" jsonschema:"description=Optional Moodle course ID; if omitted, checks all enrolled courses"`
	Since    int64 `json:"since,omitempty" jsonschema:"description=Unix timestamp to check updates since (default: 7 days ago)"`
}

type RecentChangeUpdate struct {
	Name        string `json:"name"`
	TimeUpdated int64  `json:"time_updated,omitempty"`
	ItemIDs     []int  `json:"item_ids,omitempty"`
}

type RecentChangeInstance struct {
	ContextLevel string               `json:"context_level"`
	ID           int                  `json:"id"`
	Updates      []RecentChangeUpdate `json:"updates"`
}

type RecentChangesCourse struct {
	CourseID  int                    `json:"course_id"`
	Instances []RecentChangeInstance `json:"instances"`
}

func HandleGetRecentChanges(ctx context.Context, client *api.Client, input GetRecentChangesInput) (string, error) {
	if !client.IsAuthenticated() {
		return "", api.ErrNotAuthenticated
	}

	since := input.Since
	if since <= 0 {
		since = time.Now().AddDate(0, 0, -7).Unix()
	}

	var courseIDs []int
	if input.CourseID > 0 {
		courseIDs = []int{input.CourseID}
	} else {
		ids, err := getEnrolledCourseIDs(ctx, client)
		if err != nil {
			return "", err
		}
		courseIDs = ids
	}

	var results []RecentChangesCourse
	for _, cid := range courseIDs {
		params := map[string]string{
			"courseid": fmt.Sprintf("%d", cid),
			"since":    fmt.Sprintf("%d", since),
		}
		data, err := client.Call(ctx, "core_course_get_updates_since", params)
		if err != nil {
			// Skip courses that fail availability/permission rather than aborting.
			results = append(results, RecentChangesCourse{CourseID: cid, Instances: nil})
			continue
		}

		var raw struct {
			Instances []struct {
				ContextLevel string `json:"contextlevel"`
				ID           int    `json:"id"`
				Updates      []struct {
					Name        string `json:"name"`
					TimeUpdated int64  `json:"timeupdated"`
					ItemIDs     []int  `json:"itemids"`
				} `json:"updates"`
			} `json:"instances"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return "", fmt.Errorf("parsing recent changes for course %d: %w", cid, err)
		}

		courseResult := RecentChangesCourse{CourseID: cid}
		for _, inst := range raw.Instances {
			if len(inst.Updates) == 0 {
				continue
			}
			ic := RecentChangeInstance{ContextLevel: inst.ContextLevel, ID: inst.ID}
			for _, u := range inst.Updates {
				ic.Updates = append(ic.Updates, RecentChangeUpdate{
					Name:        u.Name,
					TimeUpdated: u.TimeUpdated,
					ItemIDs:     u.ItemIDs,
				})
			}
			courseResult.Instances = append(courseResult.Instances, ic)
		}
		results = append(results, courseResult)
	}

	out, _ := json.MarshalIndent(map[string]interface{}{
		"since":   since,
		"courses": results,
	}, "", "  ")
	return string(out), nil
}
