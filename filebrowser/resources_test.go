package filebrowser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// listingResponse is a real /api/resources response for a directory, matching the
// server's iteminfo.ExtendedFileInfo. Note "modified" rather than "modTime": the
// wrong tag decodes silently to a zero time, so this shape is copied from the
// upstream struct rather than invented.
const listingResponse = `{
  "name": "photos",
  "size": 4096,
  "modified": "2026-07-20T14:02:11Z",
  "type": "directory",
  "path": "/photos",
  "folders": [
    {"name": "raw", "size": 4096, "modified": "2026-07-19T09:00:00Z", "type": "directory"},
    {"name": "2026", "size": 4096, "modified": "2026-07-18T09:00:00Z", "type": "directory"}
  ],
  "files": [
    {"name": "sunset.jpg", "size": 2048576, "modified": "2026-07-20T13:00:00Z", "type": "image/jpeg"},
    {"name": ".hidden", "size": 12, "modified": "2026-07-20T13:00:00Z", "type": "text/plain", "hidden": true},
    {"name": "beach.jpg", "size": 1024, "modified": "2026-07-20T12:00:00Z", "type": "image/jpeg"}
  ]
}`

// recordingServer captures the last request so a test can assert on the query
// parameters the client sent.
type recordingServer struct {
	lastQuery  url.Values
	lastPath   string
	lastMethod string
	lastAuth   string
	body       string
	status     int
	// captureBody, when set, is decoded from the request body. Routes that take
	// their arguments in JSON rather than in the query string are otherwise
	// unassertable, and the fields those bodies omit are as load-bearing as the
	// ones they carry.
	captureBody any
}

func newRecordingClient(t *testing.T, backend *recordingServer) *Client {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backend.lastQuery = r.URL.Query()
		backend.lastPath = r.URL.Path
		backend.lastMethod = r.Method
		backend.lastAuth = r.Header.Get("Authorization")
		if backend.captureBody != nil {
			_ = json.NewDecoder(r.Body).Decode(backend.captureBody)
		}

		status := backend.status
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(backend.body))
	}))
	t.Cleanup(server.Close)

	client, err := New(Options{BaseURL: server.URL, Token: "test-token", Source: "files"})
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}
	return client
}

func TestStatDecodesTheServerShape(t *testing.T) {
	t.Parallel()

	backend := &recordingServer{body: listingResponse}
	client := newRecordingClient(t, backend)

	listing, err := client.Stat(context.Background(), "/photos")
	if err != nil {
		t.Fatalf("Stat() returned error: %v", err)
	}

	if !listing.IsDir() {
		t.Error("IsDir() = false for a directory response")
	}
	if listing.Path != "/photos" {
		t.Errorf("Path = %q, want /photos", listing.Path)
	}
	if len(listing.Folders) != 2 || len(listing.Files) != 3 {
		t.Fatalf("decoded %d folders and %d files, want 2 and 3", len(listing.Folders), len(listing.Files))
	}
	if listing.Files[0].Size != 2048576 {
		t.Errorf("first file size = %d, want 2048576", listing.Files[0].Size)
	}
	// A zero time here means the JSON tag is wrong, which no other assertion
	// would catch because every other field would still populate.
	if listing.Files[0].Modified.IsZero() {
		t.Error("Modified is the zero time; the JSON tag does not match the server's")
	}
	if !listing.Files[1].Hidden {
		t.Error("hidden file did not decode as hidden")
	}
}

func TestStatSendsPathSourceAndBearerToken(t *testing.T) {
	t.Parallel()

	backend := &recordingServer{body: listingResponse}
	client := newRecordingClient(t, backend)

	if _, err := client.Stat(context.Background(), "/photos"); err != nil {
		t.Fatalf("Stat() returned error: %v", err)
	}

	if backend.lastPath != "/api/resources" {
		t.Errorf("request path = %q, want /api/resources", backend.lastPath)
	}
	if got := backend.lastQuery.Get("source"); got != "files" {
		t.Errorf("source = %q, want files; every resource route requires it", got)
	}
	if got := backend.lastQuery.Get("path"); got != "/photos" {
		t.Errorf("path = %q, want /photos", got)
	}
	// The server also accepts ?auth=<token>, which would put the secret in
	// Traefik's access log. The header is the only supported carrier here.
	if backend.lastAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want a bearer token", backend.lastAuth)
	}
	if backend.lastQuery.Has("auth") {
		t.Error("token was sent as a query parameter, where it lands in the access log")
	}
}

func TestPathEncodingSurvivesTheRoundTrip(t *testing.T) {
	t.Parallel()

	// Each of these breaks a different naive encoding: # starts a fragment, + is
	// a space in form encoding, and a raw space is invalid in a URL at all.
	paths := []string{
		"/photos/holiday snap.jpg",
		"/notes/c#-design.md",
		"/data/a+b.csv",
		"/音楽/曲.flac",
		"/tricky/100%.txt",
		"/deep/with?question.txt",
		"/amp/rock&roll.mp3",
	}

	for _, remotePath := range paths {
		backend := &recordingServer{body: `{"name":"x","type":"text/plain"}`}
		client := newRecordingClient(t, backend)

		if _, err := client.Stat(context.Background(), remotePath); err != nil {
			t.Errorf("Stat(%q) returned error: %v", remotePath, err)
			continue
		}
		if got := backend.lastQuery.Get("path"); got != remotePath {
			t.Errorf("path arrived as %q, want %q", got, remotePath)
		}
	}
}

func TestListRejectsAFile(t *testing.T) {
	t.Parallel()

	backend := &recordingServer{body: `{"name":"note.md","type":"text/markdown","size":12}`}
	client := newRecordingClient(t, backend)

	_, err := client.List(context.Background(), "/note.md")
	if err == nil {
		t.Fatal("List() on a file succeeded, want an error")
	}
	var notDir *NotADirectoryError
	if !asError(err, &notDir) {
		t.Errorf("error = %v, want NotADirectoryError", err)
	}
}

func TestEntriesPutsFoldersFirstAlphabetically(t *testing.T) {
	t.Parallel()

	backend := &recordingServer{body: listingResponse}
	client := newRecordingClient(t, backend)

	listing, err := client.Stat(context.Background(), "/photos")
	if err != nil {
		t.Fatalf("Stat() returned error: %v", err)
	}

	entries := listing.Entries()
	want := []string{"2026", "raw", ".hidden", "beach.jpg", "sunset.jpg"}
	if len(entries) != len(want) {
		t.Fatalf("Entries() returned %d entries, want %d", len(entries), len(want))
	}
	for i, name := range want {
		if entries[i].Name != name {
			t.Errorf("entry %d = %q, want %q", i, entries[i].Name, name)
		}
	}
}

func TestMkdirAsksForADirectory(t *testing.T) {
	t.Parallel()

	backend := &recordingServer{}
	client := newRecordingClient(t, backend)

	if err := client.Mkdir(context.Background(), "/new/deep"); err != nil {
		t.Fatalf("Mkdir() returned error: %v", err)
	}
	if backend.lastMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", backend.lastMethod)
	}
	// Without isDir the same route uploads a file, and the request body is empty,
	// so the mistake creates a zero-byte file with the directory's name.
	if got := backend.lastQuery.Get("isDir"); got != "true" {
		t.Errorf("isDir = %q, want true", got)
	}
}

func TestMoveReportsAFailureHiddenInsideA200(t *testing.T) {
	t.Parallel()

	// The route answers 200 with a per-item verdict rather than failing the
	// request, so a client that only checks the status reports success on a move
	// that did not happen.
	backend := &recordingServer{body: `{
	  "succeeded": [],
	  "failed": [{"fromPath": "/a.txt", "toPath": "/b.txt", "message": "destination exists"}]
	}`}
	client := newRecordingClient(t, backend)

	err := client.Move(context.Background(), "/a.txt", "/b.txt", false)
	if err == nil {
		t.Fatal("Move() succeeded despite a failed item in the response")
	}
	var opErr *OperationError
	if !asError(err, &opErr) {
		t.Fatalf("error = %v, want OperationError", err)
	}
	if opErr.Message != "destination exists" {
		t.Errorf("message = %q, want the server's own message", opErr.Message)
	}
}

func TestNotFoundAndConflictAreDistinguishable(t *testing.T) {
	t.Parallel()

	notFound := &recordingServer{status: http.StatusNotFound, body: `{"status":404,"message":"file does not exist"}`}
	client := newRecordingClient(t, notFound)
	_, err := client.Stat(context.Background(), "/missing")
	if !IsNotFound(err) {
		t.Errorf("IsNotFound(%v) = false, want true", err)
	}
	if IsConflict(err) {
		t.Error("a 404 also reported as a conflict")
	}

	conflict := &recordingServer{status: http.StatusConflict}
	client = newRecordingClient(t, conflict)
	if err := client.Mkdir(context.Background(), "/exists"); !IsConflict(err) {
		t.Errorf("IsConflict(%v) = false, want true", err)
	}
}

func TestErrorMessagePrefersTheServersOwn(t *testing.T) {
	t.Parallel()

	backend := &recordingServer{status: http.StatusForbidden, body: `{"status":403,"message":"user is not allowed to delete"}`}
	client := newRecordingClient(t, backend)

	err := client.Delete(context.Background(), "/protected")
	if err == nil {
		t.Fatal("Delete() succeeded, want an error")
	}
	if !containsText(err.Error(), "user is not allowed to delete") {
		t.Errorf("error = %q, want the server's message included", err)
	}
}

func TestCleanPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  string
	}{
		{"", "/"},
		{"/", "/"},
		{"photos", "/photos"},
		{"/photos/", "/photos"},
		{"/photos//2026", "/photos/2026"},
		{"  /photos  ", "/photos"},
		{"/photos/./2026", "/photos/2026"},
		// A relative escape is normalized away rather than sent, because the server
		// rejects traversal with a 400 that reads like a permissions error.
		{"/photos/../etc", "/etc"},
		{"../../etc", "/etc"},
	}

	for _, tc := range cases {
		if got := CleanPath(tc.input); got != tc.want {
			t.Errorf("CleanPath(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
