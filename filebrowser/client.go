// Package filebrowser is a client for the FileBrowser Quantum REST API.
//
// The route table and every wire shape here were read from the server's own
// source at tag v1.5.0-stable, not from the published documentation, which is
// stale in ways that matter: it lists /api/search for what is really
// /api/tools/search, and it describes uploads as multipart form data when the
// handler copies the raw request body.
package filebrowser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// apiPrefix is where the server mounts its authenticated API mux.
const apiPrefix = "/api"

type Client struct {
	baseURL string
	token   string
	source  string
	http    *http.Client
}

type Options struct {
	BaseURL string
	Token   string
	// Source names which of Quantum's configured sources to address. Every
	// resource route requires it, so it is held on the client rather than
	// threaded through each call.
	Source string
}

func New(opts Options) (*Client, error) {
	base := strings.TrimRight(opts.BaseURL, "/")
	if base == "" {
		return nil, fmt.Errorf("no server URL")
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("invalid server URL %q: %w", opts.BaseURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("server URL %q needs an http or https scheme", opts.BaseURL)
	}

	return &Client{
		baseURL: base,
		token:   opts.Token,
		source:  opts.Source,
		// Deliberately no client-level timeout. It would apply to the whole
		// exchange, body included, and kill a legitimate multi-minute upload of a
		// large file — which is the case this tool exists for. Deadlines come from
		// the per-call context instead, so `--timeout` bounds a transfer and
		// nothing bounds it by default.
		http: &http.Client{},
	}, nil
}

// Source reports the source this client addresses.
func (c *Client) Source() string { return c.source }

// APIError carries the status and the server's own message. FileBrowser only
// writes a JSON body when a handler returns an error value; several paths return
// a bare status (the 409 on an upload that would overwrite, for one), so the
// message is often empty and the status has to carry the meaning.
type APIError struct {
	Status  int
	Method  string
	Path    string
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s %s: %d %s", e.Method, e.Path, e.Status, e.Message)
	}
	return fmt.Sprintf("%s %s: %d %s", e.Method, e.Path, e.Status, http.StatusText(e.Status))
}

func statusIs(err error, status int) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Status == status
}

// IsNotFound reports whether err is the server saying the path does not exist.
func IsNotFound(err error) bool { return statusIs(err, http.StatusNotFound) }

// IsConflict reports whether err is the server refusing to overwrite. Callers
// turn this into the advice to pass --override rather than surfacing a 409.
func IsConflict(err error) bool { return statusIs(err, http.StatusConflict) }

// IsBadRequest reports whether the server rejected the request itself. Worth a
// helper because Quantum uses it where a 404 would be expected: a share hash it
// cannot find is a 400, since the lookup fails before the handler decides
// anything is missing.
func IsBadRequest(err error) bool { return statusIs(err, http.StatusBadRequest) }

// IsUnauthorized reports whether the token was rejected, which for a long-lived
// API token almost always means it expired and needs re-minting.
func IsUnauthorized(err error) bool {
	return statusIs(err, http.StatusUnauthorized) || statusIs(err, http.StatusForbidden)
}

// IsMissingPermission reports whether the server accepted the token and refused
// the action because the account lacks a permission.
//
// The status alone cannot say that. Quantum answers 403 for several unrelated
// things — a missing permission, a private source, a path that does not exist —
// and only the permission check comes from the middleware, which returns the
// status with no error value and so writes no body at all. An empty message on a
// 403 is therefore the tell, and it is exact rather than a heuristic: every
// handler that returns 403 itself returns an error to go with it.
func IsMissingPermission(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Status == http.StatusForbidden && apiErr.Message == ""
}

// newRequest builds an authenticated request. The token travels only in the
// Authorization header: the server also accepts ?auth=<token>, but a query
// string is recorded in Traefik's access log.
func (c *Client) newRequest(ctx context.Context, method, path string, query url.Values, body io.Reader) (*http.Request, error) {
	full := c.baseURL + apiPrefix + path
	if len(query) > 0 {
		full += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, full, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// do issues a request and decodes a JSON response into out, which may be nil.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := c.newRequest(ctx, method, path, query, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		return &APIError{
			Status:  resp.StatusCode,
			Method:  method,
			Path:    path,
			Message: errorMessage(payload),
		}
	}
	if out == nil || len(payload) == 0 {
		return nil
	}
	return json.Unmarshal(payload, out)
}

// errorMessage pulls the message out of FileBrowser's error envelope, falling
// back to the raw body for the handful of routes that answer in plain text.
func errorMessage(payload []byte) string {
	var envelope struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(payload, &envelope); err == nil && envelope.Message != "" {
		return envelope.Message
	}
	text := strings.TrimSpace(string(payload))
	if len(text) > 200 {
		return text[:200] + "…"
	}
	return text
}

// query returns the base query parameters every resource route needs.
func (c *Client) query(path string) url.Values {
	return url.Values{"path": []string{path}, "source": []string{c.source}}
}
