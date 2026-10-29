package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxResponseBytes = 4 << 20

// apiClient calls /api/v1 with the bearer token.
type apiClient struct {
	base  string
	token string
	http  *http.Client
}

func newAPIClient(cfg *Config, hc *http.Client) *apiClient {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &apiClient{base: cfg.BaseURL + "/api/v1", token: cfg.Token, http: hc}
}

// apiError is a non-2xx answer from the API.
type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("GoForge API answered %d: %s", e.Status, e.Body)
}

// do sends one request and returns the response body. body may be nil.
func (c *apiClient) do(ctx context.Context, method, path string, query url.Values, body any) (json.RawMessage, error) {
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "goforge-mcp")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// The URL never carries the token, so the error is safe to return.
		return nil, fmt.Errorf("call GoForge API: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read GoForge API response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &apiError{Status: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	return raw, nil
}

// seg escapes one path segment.
func seg(s string) string { return url.PathEscape(s) }
