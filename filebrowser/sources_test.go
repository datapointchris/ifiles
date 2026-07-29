package filebrowser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// sourcesResponse is a real /api/settings/sources response. The route returns a
// map keyed by source name — not an array — and each value is an index summary
// with the name repeated inside it.
const sourcesResponse = `{
  "files": {
    "name": "files",
    "readOnly": false,
    "private": false,
    "status": "ready",
    "numDirs": 412,
    "numFiles": 9871,
    "used": 21474836480,
    "total": 107374182400
  },
  "archive": {
    "name": "archive",
    "readOnly": true,
    "private": false,
    "status": "indexing",
    "numDirs": 12,
    "numFiles": 300
  }
}`

func TestSourcesDecodesTheKeyedMap(t *testing.T) {
	t.Parallel()

	backend := &recordingServer{body: sourcesResponse}
	client := newRecordingClient(t, backend)

	sources, err := client.Sources(context.Background())
	if err != nil {
		t.Fatalf("Sources() returned error: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("decoded %d sources, want 2", len(sources))
	}

	// Sorted, so output is stable across runs — Go map iteration is not.
	if sources[0].Name != "archive" || sources[1].Name != "files" {
		t.Errorf("names = %q, %q; want them sorted", sources[0].Name, sources[1].Name)
	}
	if !sources[0].ReadOnly {
		t.Error("archive did not decode as read-only")
	}
	if sources[1].NumFiles != 9871 {
		t.Errorf("files numFiles = %d, want 9871", sources[1].NumFiles)
	}
	if sources[1].Used != 21474836480 {
		t.Errorf("files used = %d, want 21474836480", sources[1].Used)
	}
	if sources[0].Status != "indexing" {
		t.Errorf("archive status = %q, want indexing", sources[0].Status)
	}
}

func TestSourcesFallsBackToTheMapKeyForAName(t *testing.T) {
	t.Parallel()

	backend := &recordingServer{body: `{"files": {"status": "ready"}}`}
	client := newRecordingClient(t, backend)

	sources, err := client.Sources(context.Background())
	if err != nil {
		t.Fatalf("Sources() returned error: %v", err)
	}
	// An unnamed value would otherwise produce a source whose name is empty, and
	// an empty source parameter is what every resource call would then send.
	if sources[0].Name != "files" {
		t.Errorf("Name = %q, want the map key used as a fallback", sources[0].Name)
	}
}

func TestSourcesRequestPath(t *testing.T) {
	t.Parallel()

	backend := &recordingServer{body: sourcesResponse}
	client := newRecordingClient(t, backend)

	if _, err := client.Sources(context.Background()); err != nil {
		t.Fatalf("Sources() returned error: %v", err)
	}
	// This route carries no swagger annotation upstream, so it is missing from the
	// published spec and easy to assume does not exist.
	if backend.lastPath != "/api/settings/sources" {
		t.Errorf("request path = %q, want /api/settings/sources", backend.lastPath)
	}
}

func TestUnauthorizedIsRecognised(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		backend := &recordingServer{status: status, body: `{"message":"unauthorized"}`}
		client := newRecordingClient(t, backend)

		_, err := client.Sources(context.Background())
		if !IsUnauthorized(err) {
			t.Errorf("IsUnauthorized(%v) = false for status %d, want true", err, status)
		}
	}
}

func TestHealthNeedsNoValidToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"message":"ok"}`))
	}))
	t.Cleanup(server.Close)

	client, _ := New(Options{BaseURL: server.URL, Token: "", Source: "files"})
	// Separating reachability from authentication is what lets `auth status` say
	// which of the two is wrong instead of showing one 401.
	if err := client.Health(context.Background()); err != nil {
		t.Errorf("Health() returned error: %v", err)
	}
}

func TestNewRejectsAURLWithoutAScheme(t *testing.T) {
	t.Parallel()

	for _, url := range []string{"", "files.example.com", "ftp://files.example.com"} {
		if _, err := New(Options{BaseURL: url, Token: "t", Source: "files"}); err == nil {
			t.Errorf("New(%q) succeeded, want an error", url)
		}
	}
}
