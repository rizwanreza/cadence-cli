// Package api is a small client for the Cadence JSON API (/api/v1).
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Prefix is prepended to every endpoint path.
const Prefix = "/api/v1"

// Client carries the resolved base URL, token and HTTP client.
type Client struct {
	HTTP      *http.Client
	BaseURL   string
	Token     string
	UserAgent string
	// Header holds extra headers sent with every request (tests, idempotency).
	Header http.Header
}

// New builds a client with the given timeout.
func New(baseURL, token, userAgent string, timeout time.Duration) *Client {
	return &Client{HTTP: &http.Client{Timeout: timeout}, BaseURL: baseURL, Token: token, UserAgent: userAgent}
}

// Error is a non-2xx API response, parsed from the error envelope
// {"error":{"code","message","details"},"errors":[...]} when present.
type Error struct {
	Status     int
	Code       string
	Message    string
	Details    []string
	RetryAfter string
}

func (e *Error) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	if len(e.Details) > 1 || (len(e.Details) == 1 && e.Details[0] != msg) {
		extra := make([]string, 0, len(e.Details))
		for _, d := range e.Details {
			if d != msg {
				extra = append(extra, d)
			}
		}
		if len(extra) > 0 {
			msg += " (" + strings.Join(extra, "; ") + ")"
		}
	}
	switch e.Status {
	case http.StatusUnauthorized:
		if strings.Contains(msg, "cadence login") {
			return msg
		}
		return "unauthorized: " + unauthorizedHint
	case http.StatusTooManyRequests:
		if e.RetryAfter != "" {
			return fmt.Sprintf("%s — retry after %ss", msg, e.RetryAfter)
		}
	}
	return msg
}

const unauthorizedHint = "your API token is missing, revoked or invalid. Run `cadence login` with a token from Settings → CLI Access"

// NetworkError wraps a transport failure (DNS, refused, timeout).
type NetworkError struct {
	Host string
	Err  error
}

func (e *NetworkError) Error() string {
	var netErr net.Error
	if errors.As(e.Err, &netErr) && netErr.Timeout() {
		return fmt.Sprintf("could not reach %s: request timed out (raise CADENCE_TIMEOUT to wait longer)", e.Host)
	}
	return fmt.Sprintf("could not reach %s: %v", e.Host, unwrapURLError(e.Err))
}

func (e *NetworkError) Unwrap() error { return e.Err }

func unwrapURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

// IsStatus reports whether err is an API error with the given status.
func IsStatus(err error, status int) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.Status == status
}

// ParseError builds an *Error from a response body. It understands the v1
// envelope, the legacy {"errors":[...]} and {"error":"..."} shapes, and plain
// text.
func ParseError(status int, body []byte) *Error {
	apiErr := &Error{Status: status}
	var envelope struct {
		Error  json.RawMessage `json:"error"`
		Errors json.RawMessage `json:"errors"`
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '{' && json.Unmarshal(trimmed, &envelope) == nil {
		if len(envelope.Error) > 0 {
			var structured struct {
				Code    string          `json:"code"`
				Message string          `json:"message"`
				Details json.RawMessage `json:"details"`
			}
			var plain string
			if json.Unmarshal(envelope.Error, &structured) == nil && (structured.Code != "" || structured.Message != "") {
				apiErr.Code = structured.Code
				apiErr.Message = structured.Message
				apiErr.Details = stringList(structured.Details)
			} else if json.Unmarshal(envelope.Error, &plain) == nil {
				apiErr.Message = plain
			}
		}
		if len(apiErr.Details) == 0 && len(envelope.Errors) > 0 {
			apiErr.Details = stringList(envelope.Errors)
		}
		if apiErr.Message == "" && len(apiErr.Details) > 0 {
			apiErr.Message = strings.Join(apiErr.Details, "; ")
		}
	} else if len(trimmed) > 0 && !bytes.HasPrefix(trimmed, []byte("<")) {
		apiErr.Message = string(trimmed)
	}
	if apiErr.Code == "" {
		apiErr.Code = codeForStatus(status)
	}
	return apiErr
}

// stringList flattens details that may be strings, objects or nested maps
// (plan validation errors) into human strings.
func stringList(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var strs []string
	if json.Unmarshal(raw, &strs) == nil {
		return strs
	}
	var anyList []any
	if json.Unmarshal(raw, &anyList) == nil {
		out := make([]string, 0, len(anyList))
		for _, item := range anyList {
			out = append(out, flatten(item))
		}
		return out
	}
	var anyMap map[string]any
	if json.Unmarshal(raw, &anyMap) == nil {
		out := []string{}
		for key, value := range anyMap {
			out = append(out, key+": "+flatten(value))
		}
		return out
	}
	return nil
}

func flatten(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case map[string]any:
		if msg, ok := value["message"].(string); ok {
			if field, ok := value["field"].(string); ok && field != "" {
				return field + ": " + msg
			}
			return msg
		}
	}
	data, _ := json.Marshal(v)
	return string(data)
}

func codeForStatus(status int) string {
	switch status {
	case 400:
		return "bad_request"
	case 401:
		return "unauthorized"
	case 403:
		return "forbidden"
	case 404:
		return "not_found"
	case 409:
		return "conflict"
	case 422:
		return "validation_failed"
	case 429:
		return "rate_limited"
	}
	if status >= 500 {
		return "server_error"
	}
	return "error"
}

// NewRequest builds a request against Prefix+path with auth and identity headers.
func (c *Client) NewRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+Prefix+path, body)
	if err != nil {
		return nil, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	req.Header.Set("Accept", "application/json")
	for key, values := range c.Header {
		for _, v := range values {
			req.Header.Add(key, v)
		}
	}
	return req, nil
}

// Do sends the request. Non-2xx responses become *Error; transport failures
// become *NetworkError.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, &NetworkError{Host: c.BaseURL, Err: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 65536))
		apiErr := ParseError(resp.StatusCode, data)
		apiErr.RetryAfter = resp.Header.Get("Retry-After")
		return nil, apiErr
	}
	return resp, nil
}

// RequestJSON sends body (if any) as JSON and decodes the response into
// result (if non-nil and the response has a body).
func (c *Client) RequestJSON(ctx context.Context, method, path string, body, result any) error {
	return c.RequestJSONWithHeaders(ctx, method, path, nil, body, result)
}

// RequestJSONWithHeaders is RequestJSON with per-request headers.
func (c *Client) RequestJSONWithHeaders(ctx context.Context, method, path string, headers http.Header, body, result any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := c.NewRequest(ctx, method, path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, values := range headers {
		for _, v := range values {
			req.Header.Set(key, v)
		}
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if result == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return &NetworkError{Host: c.BaseURL, Err: err}
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, result); err != nil {
		return fmt.Errorf("unexpected response from %s %s: %w", method, path, err)
	}
	return nil
}

// remarshal converts a decoded JSON map into a typed struct.
func remarshal(in any, out any) error {
	data, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
