package filebrowser

import (
	"context"
	"net/http"
	"sort"
)

// Source is one configured Quantum source. Quantum is multi-source and every
// resource route takes a source name, so this is what makes the rest of the API
// addressable.
type Source struct {
	Name     string `json:"name"`
	ReadOnly bool   `json:"readOnly"`
	Private  bool   `json:"private"`
	// Status is the index state: "ready", "indexing", or "unavailable". A source
	// mid-index answers listings from a partial index rather than failing, so it
	// is worth showing.
	Status   string `json:"status"`
	NumFiles uint64 `json:"numFiles"`
	NumDirs  uint64 `json:"numDirs"`
	// Used and Total are the backing filesystem's usage, not the source's own —
	// upstream names them "used" and "total".
	Used  uint64 `json:"used"`
	Total uint64 `json:"total"`
}

// Sources lists the sources the authenticated user can reach.
//
// This route has no swagger annotation upstream, so it is absent from the
// published spec; it exists in the router and is the only endpoint that both
// verifies a token and reveals a source name. That combination is what makes it
// the probe `ifiles auth login` uses.
func (c *Client) Sources(ctx context.Context) ([]Source, error) {
	// Keyed by source name, with the name repeated inside each value.
	var byName map[string]Source
	if err := c.do(ctx, http.MethodGet, "/settings/sources", nil, nil, &byName); err != nil {
		return nil, err
	}

	sources := make([]Source, 0, len(byName))
	for name, source := range byName {
		if source.Name == "" {
			source.Name = name
		}
		sources = append(sources, source)
	}
	sort.Slice(sources, func(a, b int) bool { return sources[a].Name < sources[b].Name })
	return sources, nil
}

// Health probes the server without authenticating. Used to tell "the server is
// down" apart from "the token is bad", which are the same failure to a caller
// looking at one 401.
func (c *Client) Health(ctx context.Context) error {
	req, err := c.newRequest(ctx, http.MethodGet, "/health", nil, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return &APIError{Status: resp.StatusCode, Method: http.MethodGet, Path: "/health"}
	}
	return nil
}
