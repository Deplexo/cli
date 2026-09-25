package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const DefaultOrigin = "https://deplexo.com"
const MaxResponseBytes = 8 << 20

type Client struct {
	origin string
	http   *http.Client
}

func NormalizeOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.Opaque != "" {
		return "", errors.New("API origin must be an HTTPS origin without a path, query, or user information")
	}
	u.Host = strings.ToLower(u.Host)
	if u.Port() == "443" {
		u.Host = strings.TrimSuffix(u.Host, ":443")
	}
	u.Path = ""
	return u.String(), nil
}

func New(origin string, transport http.RoundTripper) (*Client, error) {
	origin, err := NormalizeOrigin(origin)
	if err != nil {
		return nil, err
	}
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &Client{origin: origin, http: &http.Client{Transport: transport, Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (c *Client) Origin() string { return c.origin }

func (c *Client) ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return errors.New("server returned an untrusted URL")
	}
	if u.Scheme+"://"+u.Host != c.origin {
		return errors.New("server returned a URL outside the selected API origin")
	}
	return nil
}

type Error struct {
	Status     int
	Code       string
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	switch e.Status {
	case 401:
		return "sign-in required; run `deplexo auth login` or check DEPLEXO_TOKEN"
	case 403:
		return "this credential does not have permission for this operation"
	case 404:
		return "the requested resource was not found"
	case 429:
		return "the API rate limit was reached; wait before trying again"
	}
	return fmt.Sprintf("API request failed (HTTP %d)", e.Status)
}

type TransportError struct{ Cause error }

func (e *TransportError) Error() string {
	return "could not complete the API request; check your connection"
}
func (e *TransportError) Unwrap() error { return e.Cause }

func retryAfter(raw string) time.Duration {
	if n, err := strconv.ParseInt(raw, 10, 32); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(raw); err == nil {
		return max(0, time.Until(t))
	}
	return 0
}

func ValidToken(s string) bool {
	if len(s) == 0 || len(s) > 4096 {
		return false
	}
	for _, r := range s {
		if r <= 32 || r >= 127 {
			return false
		}
	}
	return true
}

func (c *Client) request(ctx context.Context, method, endpoint, token, contentType string, body io.Reader, out any) error {
	if err := c.ValidateURL(endpoint); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return errors.New("could not create the API request")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "deplexo-cli")
	if token != "" {
		if !ValidToken(token) {
			return errors.New("credential has an invalid format")
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return &TransportError{Cause: err}
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return &TransportError{Cause: err}
	}
	if len(data) > MaxResponseBytes {
		return errors.New("API response exceeds the size limit")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var payload struct {
			Error json.RawMessage `json:"error"`
		}
		var code string
		if json.Unmarshal(data, &payload) == nil {
			if json.Unmarshal(payload.Error, &code) != nil {
				var nested struct {
					Code string `json:"code"`
				}
				_ = json.Unmarshal(payload.Error, &nested)
				code = nested.Code
			}
		}
		// Only protocol decisions use the server code. Error text never echoes a response body.
		return &Error{Status: resp.StatusCode, Code: code, RetryAfter: retryAfter(resp.Header.Get("Retry-After"))}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return errors.New("API returned an invalid JSON response")
	}
	return nil
}

func (c *Client) get(ctx context.Context, path, token string, out any) error {
	return c.request(ctx, http.MethodGet, c.origin+"/user/api/v1"+path, token, "", nil, out)
}

func (c *Client) post(ctx context.Context, path, token string, in, out any) error {
	data, err := json.Marshal(in)
	if err != nil {
		return errors.New("could not encode the API request")
	}
	return c.request(ctx, http.MethodPost, c.origin+"/user/api/v1"+path, token, "application/json", bytes.NewReader(data), out)
}
