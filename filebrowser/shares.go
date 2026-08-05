package filebrowser

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"
)

// Share is one public share link.
//
// Two fields do not survive a round trip unchanged, and both are the server's
// doing. Source is answered as the source *name*, while every other route deals
// in the backend path — shareListHandler overrides it on the way out. Path is the
// scoped path: the server joins the account's scope onto whatever was asked for
// and appends a trailing slash, so a share created for /photos comes back as
// /photos/.
type Share struct {
	Hash      string `json:"hash"`
	Path      string `json:"path"`
	Source    string `json:"source"`
	Username  string `json:"username,omitempty"`
	ShareType string `json:"shareType"`
	// Expire is a Unix second, and 0 means the link never expires. Read it through
	// ExpiresAt rather than comparing the number.
	Expire         int64 `json:"expire"`
	Downloads      int   `json:"downloads"`
	DownloadsLimit int   `json:"downloadsLimit,omitempty"`
	HasPassword    bool  `json:"hasPassword,omitempty"`
	// ShareURL is the page to send someone; DownloadURL streams the content
	// directly. Both are built by the server from its own external URL, so they
	// carry the public hostname rather than whatever this client connected to.
	ShareURL    string `json:"shareURL,omitempty"`
	DownloadURL string `json:"downloadURL,omitempty"`
	// PathExists is the server stat'ing the shared path while building the
	// response. A share outlives the file it points at, so this is the only thing
	// separating a live link from one that 404s for whoever was sent it.
	PathExists bool `json:"pathExists"`
}

// ExpiresAt reports when the link stops working, or the zero time if it never
// does.
func (s Share) ExpiresAt() time.Time {
	if s.Expire == 0 {
		return time.Time{}
	}
	return time.Unix(s.Expire, 0)
}

// Expired reports whether the link has already lapsed. The server keeps expired
// shares in storage, so a listing shows them and this is what tells them apart.
func (s Share) Expired() bool {
	expiry := s.ExpiresAt()
	return !expiry.IsZero() && expiry.Before(time.Now())
}

// Shares lists every share link this account can see.
//
// An admin account sees all of them rather than only its own — the handler
// branches on the user record — so there is no parameter that narrows this.
func (c *Client) Shares(ctx context.Context) ([]Share, error) {
	var shares []Share
	if err := c.do(ctx, http.MethodGet, "/share/list", nil, nil, &shares); err != nil {
		return nil, err
	}
	sortShares(shares)
	return shares, nil
}

// SharesForPath lists the links pointing at one path.
func (c *Client) SharesForPath(ctx context.Context, remotePath string) ([]Share, error) {
	var shares []Share
	if err := c.do(ctx, http.MethodGet, "/share", c.query(remotePath), nil, &shares); err != nil {
		return nil, err
	}
	sortShares(shares)
	return shares, nil
}

// sortShares gives a stable order. The server answers in storage order, which is
// a map iteration and therefore differs between two identical requests.
func sortShares(shares []Share) {
	sort.SliceStable(shares, func(a, b int) bool {
		if shares[a].Path != shares[b].Path {
			return shares[a].Path < shares[b].Path
		}
		return shares[a].Hash < shares[b].Hash
	})
}

// ShareRequest is what a new link is created from. The server's CreateBody
// carries thirty more fields — banners, themes, sidebar links, view modes — and
// all of them are web-UI presentation, so they are left at their zero values.
type ShareRequest struct {
	Path string
	// Password is optional. The server bcrypts it and mints a URL-safe token
	// beside it, which is what a protected share's download URL carries.
	Password string
	// Expires is how long the link lives. Zero means it never expires.
	Expires time.Duration
	// DownloadsLimit caps total downloads across everyone holding the link. Zero
	// means unlimited.
	DownloadsLimit int
}

type shareCreateBody struct {
	Path           string `json:"path"`
	Source         string `json:"source"`
	Password       string `json:"password,omitempty"`
	Expires        string `json:"expires,omitempty"`
	Unit           string `json:"unit,omitempty"`
	DownloadsLimit int    `json:"downloadsLimit,omitempty"`
}

// CreateShare makes a new link for a path.
func (c *Client) CreateShare(ctx context.Context, request ShareRequest) (*Share, error) {
	body := shareCreateBody{
		Path: request.Path,
		// The name, not the backend path: this is the one create field the server
		// resolves through NameToSource rather than taking literally.
		Source:         c.source,
		Password:       request.Password,
		DownloadsLimit: request.DownloadsLimit,
	}
	if request.Expires > 0 {
		// Always seconds, whatever unit the caller was thinking in. The server
		// switches on this string and falls through to hours for anything it does
		// not recognize, so a typo would not error — it would quietly hand out a
		// link lasting twenty-four times too long.
		seconds := int64(request.Expires.Round(time.Second) / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		body.Expires = strconv.FormatInt(seconds, 10)
		body.Unit = "seconds"
	}

	var share Share
	if err := c.do(ctx, http.MethodPost, "/share", nil, body, &share); err != nil {
		return nil, err
	}
	return &share, nil
}

// DeleteShare revokes a link by hash.
func (c *Client) DeleteShare(ctx context.Context, hash string) error {
	return c.do(ctx, http.MethodDelete, "/share", url.Values{"hash": []string{hash}}, nil, nil)
}
