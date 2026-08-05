package filebrowser

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// shareListResponse is the shape /api/share/list answers with. The fields are
// share.Link's, flattened by the embedded CommonShare, with source overridden to
// the source name and username and pathExists added by ShareResponse.
const shareListResponse = `[
  {
    "hash": "T7bQ3xkLm2pR8vN4wZ",
    "path": "/photos/wedding/",
    "source": "files",
    "username": "chris",
    "shareType": "normal",
    "expire": 1786000000,
    "downloads": 3,
    "downloadsLimit": 10,
    "hasPassword": true,
    "shareURL": "https://files.example.com/public/share/T7bQ3xkLm2pR8vN4wZ",
    "downloadURL": "https://files.example.com/public/api/resources/download?hash=T7bQ3xkLm2pR8vN4wZ",
    "pathExists": true
  },
  {
    "hash": "A1bC2dE3fG4hI5jK6L",
    "path": "/archive/",
    "source": "files",
    "username": "chris",
    "shareType": "normal",
    "expire": 0,
    "downloads": 0,
    "shareURL": "https://files.example.com/public/share/A1bC2dE3fG4hI5jK6L",
    "pathExists": false
  }
]`

func TestSharesDecodesTheListShape(t *testing.T) {
	t.Parallel()

	backend := &recordingServer{body: shareListResponse}
	client := newRecordingClient(t, backend)

	shares, err := client.Shares(context.Background())
	if err != nil {
		t.Fatalf("Shares() returned error: %v", err)
	}
	if len(shares) != 2 {
		t.Fatalf("decoded %d shares, want 2", len(shares))
	}
	if backend.lastPath != "/api/share/list" {
		t.Errorf("request path = %q, want /api/share/list", backend.lastPath)
	}

	// Sorted by path, so output is stable: the server answers in storage order,
	// which is a map iteration and differs between two identical requests.
	if shares[0].Path != "/archive/" {
		t.Errorf("first path = %q, want the listing sorted by path", shares[0].Path)
	}

	wedding := shares[1]
	if wedding.Hash != "T7bQ3xkLm2pR8vN4wZ" {
		t.Errorf("Hash = %q", wedding.Hash)
	}
	if !wedding.HasPassword {
		t.Error("hasPassword did not decode")
	}
	if wedding.Downloads != 3 || wedding.DownloadsLimit != 10 {
		t.Errorf("downloads = %d/%d, want 3/10", wedding.Downloads, wedding.DownloadsLimit)
	}
	// The one field answered as a source name rather than as the backend path
	// every other route deals in.
	if wedding.Source != "files" {
		t.Errorf("Source = %q, want the source name", wedding.Source)
	}
}

func TestShareExpiryZeroMeansNever(t *testing.T) {
	t.Parallel()

	backend := &recordingServer{body: shareListResponse}
	client := newRecordingClient(t, backend)

	shares, err := client.Shares(context.Background())
	if err != nil {
		t.Fatalf("Shares() returned error: %v", err)
	}

	// The server spells "never expires" as an expire of 0, not as a null, so a
	// naive time.Unix would render 1970 and a listing would call it expired.
	never := shares[0]
	if !never.ExpiresAt().IsZero() {
		t.Errorf("ExpiresAt() = %v for expire 0, want the zero time", never.ExpiresAt())
	}
	if never.Expired() {
		t.Error("Expired() = true for a link that never expires")
	}

	dated := shares[1]
	if dated.ExpiresAt().Unix() != 1786000000 {
		t.Errorf("ExpiresAt() = %v, want the expire claim", dated.ExpiresAt())
	}
}

func TestSharesForPathSendsPathAndSource(t *testing.T) {
	t.Parallel()

	backend := &recordingServer{body: `[]`}
	client := newRecordingClient(t, backend)

	if _, err := client.SharesForPath(context.Background(), "/photos"); err != nil {
		t.Fatalf("SharesForPath() returned error: %v", err)
	}
	if backend.lastPath != "/api/share" {
		t.Errorf("request path = %q, want /api/share", backend.lastPath)
	}
	if got := backend.lastQuery.Get("path"); got != "/photos" {
		t.Errorf("path = %q, want /photos", got)
	}
	// Every resource route requires it, and this one is no exception even though
	// the response carries the source back.
	if got := backend.lastQuery.Get("source"); got != "files" {
		t.Errorf("source = %q, want files", got)
	}
}

// TestCreateShareSendsExpiryInSeconds pins the unit. The server switches on the
// unit string and falls through to hours for anything it does not recognize, so
// sending "days" with a typo would not error — it would hand out a link lasting
// twenty-four times too long.
func TestCreateShareSendsExpiryInSeconds(t *testing.T) {
	t.Parallel()

	var sent shareCreateBody
	backend := &recordingServer{body: `{"hash":"abc","path":"/x/"}`, captureBody: &sent}
	client := newRecordingClient(t, backend)

	_, err := client.CreateShare(context.Background(), ShareRequest{
		Path:    "/photos/wedding",
		Expires: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("CreateShare() returned error: %v", err)
	}

	if sent.Unit != "seconds" {
		t.Errorf("unit = %q, want seconds", sent.Unit)
	}
	if sent.Expires != "604800" {
		t.Errorf("expires = %q, want 604800", sent.Expires)
	}
	// The name, not the backend path: this is the one create field the server
	// resolves through NameToSource rather than taking literally.
	if sent.Source != "files" {
		t.Errorf("source = %q, want the source name", sent.Source)
	}
}

func TestCreateShareOmitsExpiryWhenItNeverExpires(t *testing.T) {
	t.Parallel()

	var sent shareCreateBody
	backend := &recordingServer{body: `{"hash":"abc"}`, captureBody: &sent}
	client := newRecordingClient(t, backend)

	if _, err := client.CreateShare(context.Background(), ShareRequest{Path: "/x"}); err != nil {
		t.Fatalf("CreateShare() returned error: %v", err)
	}
	// The server only sets an expiry when the field is non-empty. Sending "0"
	// would parse, add nothing to time.Now, and produce a link already expired.
	if sent.Expires != "" {
		t.Errorf("expires = %q, want it omitted", sent.Expires)
	}
	if sent.Unit != "" {
		t.Errorf("unit = %q, want it omitted", sent.Unit)
	}
}

// TestCreateShareNeverAsksForAZeroSecondLink guards the rounding. A sub-second
// duration would truncate to 0, which the server accepts and turns into a link
// that expired the moment it was made.
func TestCreateShareNeverAsksForAZeroSecondLink(t *testing.T) {
	t.Parallel()

	var sent shareCreateBody
	backend := &recordingServer{body: `{"hash":"abc"}`, captureBody: &sent}
	client := newRecordingClient(t, backend)

	_, err := client.CreateShare(context.Background(), ShareRequest{Path: "/x", Expires: time.Millisecond})
	if err != nil {
		t.Fatalf("CreateShare() returned error: %v", err)
	}
	if sent.Expires != "1" {
		t.Errorf("expires = %q, want 1", sent.Expires)
	}
}

func TestDeleteShareSendsTheHash(t *testing.T) {
	t.Parallel()

	backend := &recordingServer{}
	client := newRecordingClient(t, backend)

	if err := client.DeleteShare(context.Background(), "T7bQ3xk"); err != nil {
		t.Fatalf("DeleteShare() returned error: %v", err)
	}
	if backend.lastMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", backend.lastMethod)
	}
	if got := backend.lastQuery.Get("hash"); got != "T7bQ3xk" {
		t.Errorf("hash = %q, want T7bQ3xk", got)
	}
	// Deliberately absent: the delete route takes the hash alone, and a stray
	// path parameter would be the tell that c.query() was reached for by habit.
	if backend.lastQuery.Has("path") {
		t.Error("delete sent a path parameter, which this route does not take")
	}
}

// TestMissingPermissionIsToldApartFromTheOtherForbiddens is the discriminator
// the share commands rely on. Quantum answers 403 for a missing permission, a
// private source, and a path that does not exist, but only the permission check
// comes from middleware that returns the status with no error value — and so
// writes no body at all.
func TestMissingPermissionIsToldApartFromTheOtherForbiddens(t *testing.T) {
	t.Parallel()

	bare := &recordingServer{status: http.StatusForbidden, body: ""}
	if _, err := newRecordingClient(t, bare).Shares(context.Background()); !IsMissingPermission(err) {
		t.Errorf("IsMissingPermission() = false for a bodyless 403")
	}

	explained := &recordingServer{
		status: http.StatusForbidden,
		body:   `{"status":403,"message":"path not found: /nope"}`,
	}
	_, err := newRecordingClient(t, explained).Shares(context.Background())
	if IsMissingPermission(err) {
		t.Error("IsMissingPermission() = true for a 403 the handler explained")
	}
	if !containsText(err.Error(), "path not found") {
		t.Errorf("error = %v, want the server's own message preserved", err)
	}
}

// TestShareCreateBodyOmitsPresentationFields keeps the request minimal. The
// server's CreateBody carries thirty fields of web-UI presentation, and sending
// zero values for them would overwrite defaults on an edit.
func TestShareCreateBodyOmitsPresentationFields(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(shareCreateBody{Path: "/x", Source: "files"})
	if err != nil {
		t.Fatalf("Marshal() returned error: %v", err)
	}
	if got := string(encoded); got != `{"path":"/x","source":"files"}` {
		t.Errorf("body = %s, want only the fields that were set", got)
	}
}
