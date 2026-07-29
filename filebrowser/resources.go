package filebrowser

import (
	"context"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"
)

// DirectoryType is the value Quantum puts in an item's type field for a
// directory; every other value is a mimetype.
const DirectoryType = "directory"

// Item is one entry in a listing. Field names track the server's JSON exactly —
// note modified rather than modTime, which is the kind of detail that silently
// decodes to a zero value if guessed.
type Item struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Type     string    `json:"type"`
	Hidden   bool      `json:"hidden"`
	IsShared bool      `json:"isShared,omitempty"`
}

func (i Item) IsDir() bool { return i.Type == DirectoryType }

// Listing is the response to a GET on a directory. Quantum returns children
// split into two arrays rather than one, so a caller wanting a single ordered
// listing uses Entries.
type Listing struct {
	Item
	Path    string `json:"path"`
	Files   []Item `json:"files"`
	Folders []Item `json:"folders"`
}

// Entries returns folders then files, each alphabetical — the order the web UI
// shows, so a listing read in the terminal matches one read in the browser.
func (l *Listing) Entries() []Item {
	byName := func(items []Item) []Item {
		sorted := make([]Item, len(items))
		copy(sorted, items)
		sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].Name < sorted[b].Name })
		return sorted
	}

	entries := make([]Item, 0, len(l.Folders)+len(l.Files))
	entries = append(entries, byName(l.Folders)...)
	entries = append(entries, byName(l.Files)...)
	return entries
}

// Stat returns metadata for one path. On a directory the response also carries
// the children, which is why List and Stat hit the same route.
func (c *Client) Stat(ctx context.Context, remotePath string) (*Listing, error) {
	var listing Listing
	query := c.query(remotePath)
	// The extended attributes drive thumbnails and share badges in the web UI and
	// cost an extra index walk; nothing in a terminal listing reads them.
	query.Set("skipExtendedAttrs", "true")
	if err := c.do(ctx, http.MethodGet, "/resources", query, nil, &listing); err != nil {
		return nil, err
	}
	return &listing, nil
}

// List returns the children of a directory.
func (c *Client) List(ctx context.Context, remotePath string) (*Listing, error) {
	listing, err := c.Stat(ctx, remotePath)
	if err != nil {
		return nil, err
	}
	if !listing.IsDir() {
		return nil, &NotADirectoryError{Path: remotePath}
	}
	return listing, nil
}

// NotADirectoryError is returned when a listing is asked for a file.
type NotADirectoryError struct {
	Path string
}

func (e *NotADirectoryError) Error() string { return e.Path + " is not a directory" }

// Mkdir creates a directory. The server creates intermediate directories itself,
// so there is no -p equivalent to implement.
func (c *Client) Mkdir(ctx context.Context, remotePath string) error {
	query := c.query(remotePath)
	query.Set("isDir", "true")
	return c.do(ctx, http.MethodPost, "/resources", query, nil, nil)
}

// Delete removes a file or directory.
func (c *Client) Delete(ctx context.Context, remotePath string) error {
	return c.do(ctx, http.MethodDelete, "/resources", c.query(remotePath), nil, nil)
}

// moveCopyItem mirrors the server's MoveCopyItem. The source and destination
// each carry their own source name, because Quantum can move between sources.
type moveCopyItem struct {
	FromSource string `json:"fromSource"`
	FromPath   string `json:"fromPath"`
	ToSource   string `json:"toSource"`
	ToPath     string `json:"toPath"`
	Message    string `json:"message,omitempty"`
}

type moveCopyRequest struct {
	Items     []moveCopyItem `json:"items"`
	Action    string         `json:"action"`
	Overwrite bool           `json:"overwrite"`
	Rename    bool           `json:"rename"`
}

type moveCopyResponse struct {
	Succeeded []moveCopyItem `json:"succeeded"`
	Failed    []moveCopyItem `json:"failed"`
}

// Move relocates a path server-side, with no round trip through the client.
func (c *Client) Move(ctx context.Context, from, to string, overwrite bool) error {
	return c.moveCopy(ctx, "move", from, to, overwrite)
}

// Copy duplicates a path server-side.
func (c *Client) Copy(ctx context.Context, from, to string, overwrite bool) error {
	return c.moveCopy(ctx, "copy", from, to, overwrite)
}

func (c *Client) moveCopy(ctx context.Context, action, from, to string, overwrite bool) error {
	body := moveCopyRequest{
		Items: []moveCopyItem{{
			FromSource: c.source,
			FromPath:   from,
			ToSource:   c.source,
			ToPath:     to,
		}},
		Action:    action,
		Overwrite: overwrite,
	}

	var result moveCopyResponse
	if err := c.do(ctx, http.MethodPatch, "/resources", nil, body, &result); err != nil {
		return err
	}
	// The route answers 200 with a per-item verdict rather than failing the
	// request, so a failure here is invisible unless the body is inspected.
	if len(result.Failed) > 0 {
		return &OperationError{Action: action, Path: from, Message: result.Failed[0].Message}
	}
	return nil
}

// OperationError is a per-item failure reported inside a 2xx response body.
type OperationError struct {
	Action  string
	Path    string
	Message string
}

func (e *OperationError) Error() string {
	if e.Message == "" {
		return e.Action + " failed for " + e.Path
	}
	return e.Action + " " + e.Path + ": " + e.Message
}

// CleanPath normalizes a user-typed remote path to the absolute, slash-rooted
// form the server expects. Every route sanitizes its path argument and rejects
// traversal, so sending a relative path is a 400 rather than a resolution.
func CleanPath(remotePath string) string {
	if remotePath == "" {
		return "/"
	}
	cleaned := path.Clean("/" + strings.TrimSpace(remotePath))
	return cleaned
}
