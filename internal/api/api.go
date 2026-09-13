// Package api talks to the Bachs HTTP API.
//
// Only the handful of calls the CLI needs. Errors carry the server's own
// message rather than a bare status code: a bad event name or a key missing
// webhooks:write is worth reading, and hiding it behind "400" wastes the
// user's time.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/bachsdev/bachs-cli/internal/config"
)

// Version is the CLI version, overridden at build time via -ldflags. Set by
// main before any request is made, so the user agent is computed per call
// rather than frozen at init.
var Version = "dev"

func userAgent() string {
	return "Bachs-CLI/" + Version
}

type Client struct {
	cfg  config.Config
	http *http.Client
}

func New(cfg config.Config) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// Error is an API error with the server's own detail preserved.
type Error struct {
	StatusCode int
	Detail     string
	Code       string
}

func (e *Error) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s (%s)", e.Detail, e.Code)
	}
	return e.Detail
}

type errorBody struct {
	Detail    string `json:"detail"`
	ErrorCode string `json:"error_code"`
}

func (c *Client) do(
	ctx context.Context, method, path string, body any, out any,
) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent())

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		apiErr := &Error{StatusCode: resp.StatusCode}
		var parsed errorBody
		if json.Unmarshal(raw, &parsed) == nil && parsed.Detail != "" {
			apiErr.Detail = parsed.Detail
			apiErr.Code = parsed.ErrorCode
		} else {
			// Not JSON, or JSON without a detail: show what arrived rather
			// than inventing a message.
			apiErr.Detail = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(raw))
		}
		return apiErr
	}

	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// --- Listen sessions ---

type CreateSessionRequest struct {
	DeviceName string   `json:"device_name,omitempty"`
	ForwardTo  string   `json:"forward_to,omitempty"`
	Events     []string `json:"events,omitempty"`
}

type CreateSessionResponse struct {
	SessionID     string   `json:"session_id"`
	Token         string   `json:"token"`
	SigningSecret string   `json:"signing_secret"`
	WebSocketPath string   `json:"websocket_path"`
	Events        []string `json:"events"`
	// How often to cycle the connection. Server-dictated so deploys can
	// drain sockets without stranding them.
	ReconnectAfterSeconds int `json:"reconnect_after_seconds"`
}

func (c *Client) CreateSession(
	ctx context.Context, req CreateSessionRequest,
) (*CreateSessionResponse, error) {
	var out CreateSessionResponse
	err := c.do(ctx, http.MethodPost, "/v1/webhooks/listen/sessions", req, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// CloseSession releases a session so fanout stops aiming at a socket nobody
// holds. Best-effort at shutdown — the server's reaper covers the case where
// this never runs, which is why a failure here is not worth surfacing.
func (c *Client) CloseSession(ctx context.Context, sessionID string) error {
	return c.do(
		ctx, http.MethodDelete,
		"/v1/webhooks/listen/sessions/"+sessionID, nil, nil,
	)
}

// --- Replay ---

type ReplayRequest struct {
	EventID string `json:"event_id,omitempty"`
}

type ReplayResponse struct {
	EventID   string `json:"event_id"`
	AttemptID string `json:"attempt_id"`
	AttemptNo int    `json:"attempt_no"`
	EventType string `json:"event_type"`
}

func (c *Client) Replay(
	ctx context.Context, eventID string,
) (*ReplayResponse, error) {
	var out ReplayResponse
	err := c.do(
		ctx, http.MethodPost, "/v1/webhooks/replay",
		ReplayRequest{EventID: eventID}, &out,
	)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// --- Events ---

// Event is one webhook event and a summary of how its delivery went.
type Event struct {
	EventID    string `json:"event_id"`
	EventType  string `json:"event_type"`
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	CreatedAt  string `json:"created_at"`
	Account    string `json:"account"`

	// Attempts is 0 when nothing was configured to receive the event. That
	// distinction matters: it separates "nobody was listening" from "delivery
	// was tried and failed", which otherwise look identical from outside.
	Attempts int `json:"attempts"`
	Success  int `json:"success"`
	Failed   int `json:"failed"`

	LastAttemptStatus     *string `json:"last_attempt_status"`
	LastAttemptHTTPStatus *int    `json:"last_attempt_http_status"`
	LastAttemptError      *string `json:"last_attempt_error"`
	LastAttemptAt         *string `json:"last_attempt_at"`
}

type EventsResponse struct {
	Items  []Event `json:"items"`
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
}

// ListEvents returns a page of recent events, newest first.
//
// The server takes only limit and offset, so any filtering by type or outcome
// happens in the caller. Pages are capped at 100 server-side.
func (c *Client) ListEvents(
	ctx context.Context, limit, offset int,
) (*EventsResponse, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	var out EventsResponse
	path := fmt.Sprintf("/v1/webhooks/events?limit=%d&offset=%d", limit, offset)
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// --- Endpoints ---

type Endpoint struct {
	EndpointID  string   `json:"endpoint_id"`
	Name        string   `json:"name"`
	URL         *string  `json:"url"`
	Enabled     bool     `json:"enabled"`
	EventTypes  []string `json:"event_types"`
	EventSource string   `json:"event_source"`
	CreatedAt   string   `json:"created_at"`
}

type endpointsResponse struct {
	Items []Endpoint `json:"items"`
}

func (c *Client) ListEndpoints(ctx context.Context) ([]Endpoint, error) {
	var out endpointsResponse
	if err := c.do(ctx, http.MethodGet, "/v1/webhooks/endpoints", nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

type CreateEndpointRequest struct {
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	EventTypes []string `json:"event_types"`
}

// CreateEndpointResponse carries the signing secret, which the API returns
// only on creation.
type CreateEndpointResponse struct {
	Endpoint
	Secret string `json:"secret"`
}

func (c *Client) CreateEndpoint(
	ctx context.Context, req CreateEndpointRequest,
) (*CreateEndpointResponse, error) {
	var out CreateEndpointResponse
	if err := c.do(ctx, http.MethodPost, "/v1/webhooks/endpoints", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteEndpoint(ctx context.Context, endpointID string) error {
	return c.do(
		ctx, http.MethodDelete, "/v1/webhooks/endpoints/"+endpointID, nil, nil,
	)
}

// --- Listen sessions ---

type Session struct {
	SessionID  string `json:"session_id"`
	DeviceName string `json:"device_name"`
	ForwardTo  string `json:"forward_to"`
	Status     string `json:"status"`
	// Live is the truth about whether anything is holding the socket right
	// now. Status alone can lie, because a killed process never gets to
	// update it.
	Live       bool   `json:"live"`
	LastSeenAt string `json:"last_seen_at"`
	CreatedAt  string `json:"created_at"`
}

type sessionsResponse struct {
	Sessions []Session `json:"sessions"`
}

// ListSessions returns the forwarding sessions that have not finished.
func (c *Client) ListSessions(ctx context.Context) ([]Session, error) {
	var out sessionsResponse
	if err := c.do(
		ctx, http.MethodGet, "/v1/webhooks/listen/sessions", nil, &out,
	); err != nil {
		return nil, err
	}
	return out.Sessions, nil
}

// --- Trigger ---

type TriggerRequest struct {
	EventType string `json:"event_type"`
}

type TriggerResponse struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	// Destinations is 0 when nothing is configured to receive the event, which
	// is worth surfacing: the trigger succeeded but nobody heard it.
	Destinations int `json:"destinations"`
}

// Trigger emits a sample event so a handler can be exercised without making a
// real payment. Sandbox only; the server refuses in production.
func (c *Client) Trigger(
	ctx context.Context, eventType string,
) (*TriggerResponse, error) {
	var out TriggerResponse
	err := c.do(ctx, http.MethodPost, "/v1/webhooks/listen/trigger",
		TriggerRequest{EventType: eventType}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Raw performs an arbitrary request against the API.
//
// Used by the generated resource commands, where the path and method come from
// the OpenAPI spec rather than a hand-written method per endpoint.
func (c *Client) Raw(
	ctx context.Context, method, path string, body any, out any,
) error {
	return c.do(ctx, method, path, body, out)
}

// --- Device login ---

type DeviceCodeRequest struct {
	DeviceName string `json:"device_name,omitempty"`
}

type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type DeviceTokenResponse struct {
	Status         string `json:"status"`
	APIKey         string `json:"api_key"`
	OrganizationID string `json:"organization_id"`
}

// RequestDeviceCode starts a login. Unauthenticated: there is no credential yet.
func RequestDeviceCode(
	ctx context.Context, baseURL, deviceName string,
) (*DeviceCodeResponse, error) {
	c := &Client{cfg: config.Config{BaseURL: baseURL}, http: &http.Client{Timeout: 30 * time.Second}}
	var out DeviceCodeResponse
	if err := c.do(ctx, http.MethodPost, "/v1/device/code",
		DeviceCodeRequest{DeviceName: deviceName}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PollDeviceToken checks whether the login has been approved yet.
func PollDeviceToken(
	ctx context.Context, baseURL, deviceCode string,
) (*DeviceTokenResponse, error) {
	c := &Client{cfg: config.Config{BaseURL: baseURL}, http: &http.Client{Timeout: 30 * time.Second}}
	var out DeviceTokenResponse
	if err := c.do(ctx, http.MethodPost, "/v1/device/token",
		map[string]string{"device_code": deviceCode}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
