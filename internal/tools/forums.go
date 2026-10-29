package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PejarRu/mcp-aules/internal/api"
)

// --- Forum reading tool ---
// A single tool covers three navigation levels to keep MCP tool count minimal:
//   course_id  -> list forums in the course
//   forum_id   -> list discussions in that forum
//   discussion_id -> full thread posts

type ReadForumInput struct {
	CourseID     int `json:"course_id,omitempty" jsonschema:"description=Moodle course ID; lists forums in the course"`
	ForumID      int `json:"forum_id,omitempty" jsonschema:"description=Forum instance ID; lists discussions in that forum"`
	DiscussionID int `json:"discussion_id,omitempty" jsonschema:"description=Discussion ID; returns full thread posts"`
	Limit        int `json:"limit,omitempty" jsonschema:"description=Maximum discussions to return (default 20)"`
}

func HandleReadForum(ctx context.Context, client *api.Client, input ReadForumInput) (string, error) {
	if !client.IsAuthenticated() {
		return "", api.ErrNotAuthenticated
	}

	if input.DiscussionID > 0 {
		return readForumDiscussion(ctx, client, input.DiscussionID)
	}
	if input.ForumID > 0 {
		return readForumDiscussions(ctx, client, input.ForumID, input.Limit)
	}
	if input.CourseID > 0 {
		return readCourseForums(ctx, client, input.CourseID)
	}
	return "", fmt.Errorf("one of course_id, forum_id or discussion_id is required")
}

func readCourseForums(ctx context.Context, client *api.Client, courseID int) (string, error) {
	params := map[string]string{
		"courseids[0]": fmt.Sprintf("%d", courseID),
	}
	data, err := client.Call(ctx, "mod_forum_get_forums_by_courses", params)
	if err != nil {
		return "", err
	}
	var forums []map[string]any
	if err := json.Unmarshal(data, &forums); err != nil {
		return "", fmt.Errorf("parsing forums: %w", err)
	}

	type forumDisplay struct {
		ID             int    `json:"id"`
		Name           string `json:"name"`
		Type           string `json:"type"`
		NumDiscussions int    `json:"num_discussions"`
		UnreadPosts    int    `json:"unread_posts,omitempty"`
	}
	var out []forumDisplay
	for _, f := range forums {
		out = append(out, forumDisplay{
			ID:             intValue(f["id"]),
			Name:           stringValue(f["name"]),
			Type:           stringValue(f["type"]),
			NumDiscussions: intValue(f["numdiscussions"]),
			UnreadPosts:    intValue(f["unreadpostscount"]),
		})
	}
	res, _ := json.MarshalIndent(map[string]any{
		"course_id": courseID,
		"forums":    out,
	}, "", "  ")
	return string(res), nil
}

func readForumDiscussions(ctx context.Context, client *api.Client, forumID, limit int) (string, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	params := map[string]string{
		"forumid": fmt.Sprintf("%d", forumID),
		"perpage": fmt.Sprintf("%d", limit),
		"page":    "0",
	}
	data, err := client.Call(ctx, "mod_forum_get_forum_discussions", params)
	if err != nil {
		return "", err
	}
	var raw struct {
		Discussions []map[string]any `json:"discussions"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", fmt.Errorf("parsing discussions: %w", err)
	}

	type discDisplay struct {
		ID         int    `json:"id"`
		Discussion int    `json:"discussion_id"`
		Subject    string `json:"subject"`
		Author     string `json:"author"`
		Created    int64  `json:"created"`
		Date       string `json:"date"`
		Replies    int    `json:"replies"`
		Unread     int    `json:"unread"`
		Pinned     bool   `json:"pinned"`
		Locked     bool   `json:"locked"`
		Preview    string `json:"preview"`
	}
	var out []discDisplay
	for _, d := range raw.Discussions {
		msg := stripHTML(stringValue(d["message"]))
		out = append(out, discDisplay{
			ID:         intValue(d["id"]),
			Discussion: intValue(d["discussion"]),
			Subject:    stringValue(d["subject"]),
			Author:     stringValue(d["userfullname"]),
			Created:    int64Value(d["created"]),
			Date:       rfc3339(int64Value(d["created"])),
			Replies:    intValue(d["numreplies"]),
			Unread:     intValue(d["numunread"]),
			Pinned:     boolValue(d["pinned"]),
			Locked:     boolValue(d["locked"]),
			Preview:    truncate(msg, 200),
		})
	}
	res, _ := json.MarshalIndent(map[string]any{
		"forum_id":    forumID,
		"total":       len(out),
		"discussions": out,
	}, "", "  ")
	return string(res), nil
}

func readForumDiscussion(ctx context.Context, client *api.Client, discussionID int) (string, error) {
	params := map[string]string{
		"discussionid":  fmt.Sprintf("%d", discussionID),
		"sortby":        "created",
		"sortdirection": "ASC",
	}
	data, err := client.Call(ctx, "mod_forum_get_discussion_posts", params)
	if err != nil {
		return "", err
	}
	var raw struct {
		Posts    []map[string]any `json:"posts"`
		ForumID  int              `json:"forumid"`
		CourseID int              `json:"courseid"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", fmt.Errorf("parsing discussion posts: %w", err)
	}

	type postDisplay struct {
		ID          int    `json:"id"`
		Subject     string `json:"subject"`
		Author      string `json:"author"`
		Message     string `json:"message"`
		MessageHTML string `json:"message_html,omitempty"`
		Created     int64  `json:"created"`
		Date        string `json:"date"`
		ParentID    *int   `json:"parent_id,omitempty"`
		Unread      bool   `json:"unread"`
	}
	var out []postDisplay
	for _, p := range raw.Posts {
		author := ""
		if a, ok := p["author"].(map[string]any); ok {
			author = firstNonEmpty(stringValue(a["fullname"]), stringValue(a["id"]))
		}
		if author == "" {
			author = stringValue(p["userfullname"])
		}
		pd := postDisplay{
			ID:          intValue(p["id"]),
			Subject:     stringValue(p["subject"]),
			Author:      author,
			Message:     stripHTML(stringValue(p["message"])),
			MessageHTML: stringValue(p["message"]),
			Created:     int64Value(p["timecreated"]),
			Date:        rfc3339(int64Value(p["timecreated"])),
			Unread:      boolValue(p["unread"]),
		}
		if pid, ok := p["parentid"]; ok && pid != nil {
			v := intValue(pid)
			if v > 0 {
				pd.ParentID = &v
			}
		}
		out = append(out, pd)
	}
	res, _ := json.MarshalIndent(map[string]any{
		"discussion_id": discussionID,
		"forum_id":      raw.ForumID,
		"course_id":     raw.CourseID,
		"total_posts":   len(out),
		"posts":         out,
	}, "", "  ")
	return string(res), nil
}
