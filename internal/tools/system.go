package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PejarRu/mcp-aules/internal/api"
)

// --- Login Tool ---

type LoginInput struct {
	MoodleURL string `json:"moodle_url" jsonschema:"description=Your Moodle site URL (e.g. https://moodle.university.edu)"`
	Username  string `json:"username" jsonschema:"description=Your Moodle username or email"`
	Password  string `json:"password" jsonschema:"description=Your Moodle password"`
}

func HandleLogin(ctx context.Context, client *api.Client, input LoginInput) (string, error) {
	if input.MoodleURL == "" || input.Username == "" || input.Password == "" {
		return "", fmt.Errorf("moodle_url, username, and password are all required")
	}

	// Normalize URL
	baseURL := input.MoodleURL
	if baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}

	// Get token from credentials
	token, err := api.GetTokenFromCredentials(ctx, baseURL, input.Username, input.Password)
	if err != nil {
		return "", fmt.Errorf("login failed: %w", err)
	}

	// Set the session on the client
	client.SetSession(baseURL, token)

	// Fetch site info to get user ID and verify connection
	info, err := getSiteInfoRaw(ctx, client)
	if err != nil {
		return "", fmt.Errorf("login succeeded but failed to get site info: %w", err)
	}

	client.SetUserID(info.UserID)

	return fmt.Sprintf("Successfully logged in to %s as %s (%s). You now have access to all Moodle tools.",
		info.SiteName, info.FullName, info.Username), nil
}

// --- Get Site Info Tool ---

type SiteInfo struct {
	SiteName  string `json:"sitename"`
	Username  string `json:"username"`
	FullName  string `json:"fullname"`
	UserID    int    `json:"userid"`
	SiteURL   string `json:"siteurl"`
	Lang      string `json:"lang"`
	UserEmail string `json:"useremail"`
}

func getSiteInfoRaw(ctx context.Context, client *api.Client) (*SiteInfo, error) {
	data, err := client.Call(ctx, "core_webservice_get_site_info", nil)
	if err != nil {
		return nil, err
	}

	var info SiteInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("parsing site info: %w", err)
	}
	return &info, nil
}

type GetSiteInfoInput struct{}

func HandleGetSiteInfo(ctx context.Context, client *api.Client, _ GetSiteInfoInput) (string, error) {
	if !client.IsAuthenticated() {
		return "", api.ErrNotAuthenticated
	}

	info, err := getSiteInfoRaw(ctx, client)
	if err != nil {
		return "", err
	}

	result, _ := json.MarshalIndent(info, "", "  ")
	return string(result), nil
}

// --- Get User Profile Tool ---

type UserProfile struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	FullName    string `json:"fullname"`
	Email       string `json:"email"`
	Department  string `json:"department"`
	Institution string `json:"institution"`
	Description string `json:"description"`
	City        string `json:"city"`
	Country     string `json:"country"`
	ProfileURL  string `json:"profileurl"`
	ImageURL    string `json:"profileimageurl"`
}

type GetUserProfileInput struct{}

func HandleGetUserProfile(ctx context.Context, client *api.Client, _ GetUserProfileInput) (string, error) {
	if !client.IsAuthenticated() {
		return "", api.ErrNotAuthenticated
	}

	userID := client.GetUserID()
	if userID == 0 {
		// Try to get it from site info
		info, err := getSiteInfoRaw(ctx, client)
		if err != nil {
			return "", err
		}
		userID = info.UserID
		client.SetUserID(userID)
	}

	params := map[string]string{
		"field":     "id",
		"values[0]": fmt.Sprintf("%d", userID),
	}

	data, err := client.Call(ctx, "core_user_get_users_by_field", params)
	if err != nil {
		return "", err
	}

	var users []UserProfile
	if err := json.Unmarshal(data, &users); err != nil {
		return "", fmt.Errorf("parsing user profile: %w", err)
	}

	if len(users) == 0 {
		return "", fmt.Errorf("user not found")
	}

	result, _ := json.MarshalIndent(users[0], "", "  ")
	return string(result), nil
}
