package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Client is the Moodle REST API client.
type Client struct {
	mu      sync.RWMutex
	baseURL string
	token   string
	userID  int
	http    *http.Client
}

// NewClient creates a new Moodle API client.
func NewClient() *Client {
	return &Client{
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// SetSession configures the client with a Moodle URL and token.
func (c *Client) SetSession(baseURL, token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baseURL = baseURL
	c.token = token
}

// SetUserID stores the current user's Moodle ID.
func (c *Client) SetUserID(id int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.userID = id
}

// GetUserID returns the current user's Moodle ID.
func (c *Client) GetUserID() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.userID
}

// IsAuthenticated returns true if the client has a valid session.
func (c *Client) IsAuthenticated() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL != "" && c.token != ""
}

// GetBaseURL returns the configured Moodle base URL.
func (c *Client) GetBaseURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL
}

// GetToken returns the current authentication token.
func (c *Client) GetToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}

// Call makes a request to the Moodle REST API.
// function is the wsfunction name (e.g. "core_enrol_get_users_courses").
// params are additional query parameters.
func (c *Client) Call(ctx context.Context, function string, params map[string]string) (json.RawMessage, error) {
	c.mu.RLock()
	baseURL := c.baseURL
	token := c.token
	c.mu.RUnlock()

	if baseURL == "" || token == "" {
		return nil, ErrNotAuthenticated
	}

	endpoint := fmt.Sprintf("%s/webservice/rest/server.php", baseURL)

	qp := url.Values{
		"wstoken":            {token},
		"wsfunction":         {function},
		"moodlewsrestformat": {"json"},
	}
	for k, v := range params {
		qp.Set(k, v)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.URL.RawQuery = qp.Encode()

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling Moodle API (%s): %w", function, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	// Check if the response is a Moodle error object
	var apiErr struct {
		ErrorCode string `json:"errorcode"`
		Message   string `json:"message"`
		Exception string `json:"exception"`
	}
	if json.Unmarshal(body, &apiErr) == nil && apiErr.ErrorCode != "" {
		return nil, &APIError{
			ErrorCode: apiErr.ErrorCode,
			Message:   apiErr.Message,
			Exception: apiErr.Exception,
		}
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("Moodle API (%s) returned HTTP %d", function, resp.StatusCode)
	}

	return json.RawMessage(body), nil
}
