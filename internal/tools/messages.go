package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PejarRu/mcp-aules/internal/api"
)

// --- Get Notifications Tool ---

type Message struct {
	ID                int    `json:"id"`
	UserIDFrom        int    `json:"useridfrom"`
	UserFromFullName  string `json:"userfromfullname"`
	Subject           string `json:"subject"`
	Text              string `json:"text"`
	FullMessage       string `json:"fullmessage"`
	FullMessageHTML   string `json:"fullmessagehtml"`
	FullMessageFormat int    `json:"fullmessageformat"`
	SmallMessage      string `json:"smallmessage"`
	Component         string `json:"component"`
	EventType         string `json:"eventtype"`
	ContextURL        string `json:"contexturl"`
	ContextURLName    string `json:"contexturlname"`
	TimeCreated       int64  `json:"timecreated"`
	TimeRead          int64  `json:"timeread"`
}

type MessageDisplay struct {
	ID                int    `json:"id"`
	From              string `json:"from"`
	Subject           string `json:"subject"`
	Preview           string `json:"preview"`
	FullMessage       string `json:"fullmessage"`
	FullMessageHTML   string `json:"fullmessagehtml"`
	FullMessageFormat int    `json:"fullmessageformat"`
	BodyText          string `json:"body_text"`
	Component         string `json:"component,omitempty"`
	EventType         string `json:"event_type,omitempty"`
	Context           string `json:"context,omitempty"`
	Link              string `json:"link,omitempty"`
	Timestamp         int64  `json:"timestamp"`
	Date              string `json:"date"`
	Read              bool   `json:"read"`
}

type MessagesResponse struct {
	Messages []Message `json:"messages"`
}

type GetNotificationsInput struct {
	Limit      int  `json:"limit,omitempty" jsonschema:"description=Maximum number of notifications to return (default: 20)"`
	UnreadOnly bool `json:"unread_only,omitempty" jsonschema:"description=Only show unread notifications (default: true)"`
}

type GetMessagesInput struct {
	Limit      int  `json:"limit,omitempty" jsonschema:"description=Maximum number of private messages to return (default: 20)"`
	UnreadOnly bool `json:"unread_only,omitempty" jsonschema:"description=Only show unread private messages"`
}

func HandleGetMessages(ctx context.Context, client *api.Client, input GetMessagesInput) (string, error) {
	return handleGetMessages(ctx, client, input.Limit, input.UnreadOnly, "conversations")
}

func HandleGetNotifications(ctx context.Context, client *api.Client, input GetNotificationsInput) (string, error) {
	return handleGetNotifications(ctx, client, input.Limit, input.UnreadOnly)
}

// handleGetNotifications combines core_message notifications with popup notifications,
// deduplicating by id and distinguishing source. Private messages are excluded.
func handleGetNotifications(ctx context.Context, client *api.Client, requestedLimit int, unreadOnly bool) (string, error) {
	if !client.IsAuthenticated() {
		return "", api.ErrNotAuthenticated
	}

	limit := requestedLimit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	userID, err := ensureUserID(ctx, client)
	if err != nil {
		return "", err
	}

	seen := map[string]bool{}
	var display []MessageDisplay

	// 1) Core message notifications (core_message_get_messages, type=notifications)
	params := map[string]string{
		"useridto":    fmt.Sprintf("%d", userID),
		"useridfrom":  "0",
		"type":        "notifications",
		"read":        "2",
		"limitnum":    fmt.Sprintf("%d", limit),
		"newestfirst": "1",
	}
	if unreadOnly {
		params["read"] = "0"
	}
	data, err := client.Call(ctx, "core_message_get_messages", params)
	if err == nil {
		var resp MessagesResponse
		if err := json.Unmarshal(data, &resp); err == nil {
			for _, m := range resp.Messages {
				key := fmt.Sprintf("%d|%s|%d", m.ID, m.Subject, m.TimeCreated)
				if seen[key] {
					continue
				}
				seen[key] = true
				bodyText := m.FullMessage
				if bodyText == "" {
					bodyText = m.Text
				}
				bodyHTML := m.FullMessageHTML
				if bodyHTML == "" {
					bodyHTML = m.FullMessage
				}
				display = append(display, MessageDisplay{
					ID: m.ID, From: m.UserFromFullName, Subject: m.Subject,
					Preview:     truncate(stripHTML(firstNonEmpty(m.SmallMessage, bodyText)), 150),
					FullMessage: m.FullMessage, FullMessageHTML: bodyHTML, FullMessageFormat: m.FullMessageFormat,
					BodyText: stripHTML(bodyText), Component: m.Component, EventType: m.EventType, Context: m.ContextURLName,
					Link: removeToken(m.ContextURL), Timestamp: m.TimeCreated,
					Date: rfc3339(m.TimeCreated), Read: m.TimeRead > 0,
				})
				// mark source in component if empty
				if display[len(display)-1].Component == "" {
					display[len(display)-1].Component = "core_message"
				}
			}
		}
	}

	// 2) Popup notifications (message_popup_get_popup_notifications)
	popupParams := map[string]string{
		"useridto":    fmt.Sprintf("%d", userID),
		"newestfirst": "1",
		"limit":       fmt.Sprintf("%d", limit),
		"offset":      "0",
	}
	pdata, err := client.Call(ctx, "message_popup_get_popup_notifications", popupParams)
	if err == nil {
		var prow struct {
			Notifications []map[string]any `json:"notifications"`
		}
		if err := json.Unmarshal(pdata, &prow); err == nil {
			for _, n := range prow.Notifications {
				id := intValue(n["id"])
				subject := stringValue(n["subject"])
				created := int64Value(n["timecreated"])
				key := fmt.Sprintf("%d|%s|%d", id, subject, created)
				if seen[key] {
					continue
				}
				seen[key] = true
				read := boolValue(n["read"])
				if timeread := int64Value(n["timeread"]); timeread > 0 {
					read = true
				}
				if unreadOnly && read {
					continue
				}
				display = append(display, MessageDisplay{
					ID:                id,
					From:              stringValue(n["userfromfullname"]),
					Subject:           stringValue(n["subject"]),
					Preview:           truncate(stripHTML(firstNonEmpty(stringValue(n["smallmessage"]), stringValue(n["text"]))), 150),
					FullMessage:       stringValue(n["fullmessage"]),
					FullMessageHTML:   stringValue(n["fullmessagehtml"]),
					FullMessageFormat: intValue(n["fullmessageformat"]),
					BodyText:          stripHTML(firstNonEmpty(stringValue(n["text"]), stringValue(n["fullmessage"]))),
					Component:         firstNonEmpty(stringValue(n["component"]), "message_popup"),
					EventType:         stringValue(n["eventtype"]),
					Context:           stringValue(n["contexturlname"]),
					Link:              removeToken(stringValue(n["contexturl"])),
					Timestamp:         int64Value(n["timecreated"]),
					Date:              rfc3339(int64Value(n["timecreated"])),
					Read:              read,
				})
			}
		}
	}

	result, _ := json.MarshalIndent(map[string]interface{}{
		"timezone":       "Europe/Madrid",
		"total_messages": len(display),
		"notifications":  display,
		"source":         "core_message+popup",
	}, "", "  ")
	return string(result), nil
}

func handleGetMessages(ctx context.Context, client *api.Client, requestedLimit int, unreadOnly bool, messageType string) (string, error) {
	if !client.IsAuthenticated() {
		return "", api.ErrNotAuthenticated
	}

	limit := requestedLimit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	userID, err := ensureUserID(ctx, client)
	if err != nil {
		return "", err
	}
	params := map[string]string{
		"useridto":    fmt.Sprintf("%d", userID),
		"useridfrom":  "0",
		"type":        messageType,
		"read":        "2",
		"limitnum":    fmt.Sprintf("%d", limit),
		"newestfirst": "1",
	}
	if unreadOnly {
		params["read"] = "0"
	}

	data, err := client.Call(ctx, "core_message_get_messages", params)
	if err != nil {
		return "", err
	}

	var resp MessagesResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("parsing messages: %w", err)
	}

	var display []MessageDisplay
	for _, m := range resp.Messages {
		bodyText := m.FullMessage
		if bodyText == "" {
			bodyText = m.Text
		}
		bodyHTML := m.FullMessageHTML
		if bodyHTML == "" {
			bodyHTML = m.FullMessage
		}
		display = append(display, MessageDisplay{
			ID: m.ID, From: m.UserFromFullName, Subject: m.Subject,
			Preview:     truncate(stripHTML(firstNonEmpty(m.SmallMessage, bodyText)), 150),
			FullMessage: m.FullMessage, FullMessageHTML: bodyHTML, FullMessageFormat: m.FullMessageFormat,
			BodyText: stripHTML(bodyText), Component: m.Component, EventType: m.EventType, Context: m.ContextURLName,
			Link: removeToken(m.ContextURL), Timestamp: m.TimeCreated,
			Date: rfc3339(m.TimeCreated), Read: m.TimeRead > 0,
		})
	}

	result, _ := json.MarshalIndent(map[string]interface{}{
		"timezone":       "Europe/Madrid",
		"total_messages": len(display),
		"messages":       display,
	}, "", "  ")
	return string(result), nil
}
