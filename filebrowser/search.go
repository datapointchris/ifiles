package filebrowser

import (
	"context"
	"net/http"
	"net/url"
)

// SearchResult is one hit. Modified is a string, not a timestamp: the index
// stores it pre-formatted and the route passes it straight through, so decoding
// it as a time would fail on every response.
type SearchResult struct {
	Path     string `json:"path"`
	Type     string `json:"type"`
	Size     int64  `json:"size"`
	Modified string `json:"modified,omitempty"`
	Source   string `json:"source"`
}

func (r SearchResult) IsDir() bool { return r.Type == DirectoryType }

// SearchRequest describes a search.
type SearchRequest struct {
	Query string
	// Scope narrows the search to a subtree. Empty searches the whole source.
	Scope string
	// Wildcard treats the query as a glob rather than a substring match.
	Wildcard bool
}

// Search queries the server's index.
//
// The route is /tools/search. The published documentation says /search, which
// does not exist and 404s.
func (c *Client) Search(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	query := url.Values{
		"query":   []string{req.Query},
		"sources": []string{c.source},
	}
	if req.Scope != "" {
		query.Set("scope", req.Scope)
	}
	if req.Wildcard {
		query.Set("useWildcard", "true")
	}

	var results []SearchResult
	if err := c.do(ctx, http.MethodGet, "/tools/search", query, nil, &results); err != nil {
		return nil, err
	}
	return results, nil
}
