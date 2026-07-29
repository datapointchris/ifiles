package filebrowser

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestDownloadUsesFileNotPath(t *testing.T) {
	t.Parallel()

	var query url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_, _ = w.Write([]byte("payload"))
	}))
	t.Cleanup(server.Close)

	client, err := New(Options{BaseURL: server.URL, Token: "t", Source: "files"})
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	download, err := client.Download(context.Background(), DownloadRequest{Paths: []string{"/a.txt"}})
	if err != nil {
		t.Fatalf("Download() returned error: %v", err)
	}
	defer func() { _ = download.Close() }()

	// This route is the one exception to the ?path= convention every other
	// resource route follows. Sending path= yields a 400 that reads like a
	// permissions problem.
	if got := query.Get("file"); got != "/a.txt" {
		t.Errorf("file = %q, want /a.txt", got)
	}
	if query.Has("path") {
		t.Error("path= was sent; this route reads ?file= only")
	}
	if got := query.Get("source"); got != "files" {
		t.Errorf("source = %q, want files", got)
	}
}

func TestDownloadMultiplePathsBecomeRepeatedFileParams(t *testing.T) {
	t.Parallel()

	var query url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_, _ = w.Write([]byte("zipdata"))
	}))
	t.Cleanup(server.Close)

	client, _ := New(Options{BaseURL: server.URL, Token: "t", Source: "files"})
	download, err := client.Download(context.Background(), DownloadRequest{
		Paths:   []string{"/a.txt", "/b.txt"},
		Archive: ArchiveTarGz,
	})
	if err != nil {
		t.Fatalf("Download() returned error: %v", err)
	}
	defer func() { _ = download.Close() }()

	if got := query["file"]; len(got) != 2 {
		t.Errorf("file params = %v, want two", got)
	}
	if got := query.Get("algo"); got != ArchiveTarGz {
		t.Errorf("algo = %q, want %s", got, ArchiveTarGz)
	}
}

func TestDownloadRejectsAnUnsupportedArchiveFormat(t *testing.T) {
	t.Parallel()

	client, _ := New(Options{BaseURL: "https://files.example.com", Token: "t", Source: "files"})
	// The server answers 500 rather than 400 for an unknown format, so validating
	// here turns an "internal server error" into an actionable message.
	_, err := client.Download(context.Background(), DownloadRequest{
		Paths:   []string{"/dir"},
		Archive: "7z",
	})
	if err == nil {
		t.Fatal("Download() with an unsupported format succeeded, want an error")
	}
}

func TestDownloadResumeSendsARangeAndReportsHonouring(t *testing.T) {
	t.Parallel()

	const full = "0123456789abcdef"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "bytes=10-" {
			w.Header().Set("Content-Range", "bytes 10-15/16")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte(full[10:]))
			return
		}
		_, _ = w.Write([]byte(full))
	}))
	t.Cleanup(server.Close)

	client, _ := New(Options{BaseURL: server.URL, Token: "t", Source: "files"})
	download, err := client.Download(context.Background(), DownloadRequest{
		Paths:  []string{"/a.txt"},
		Offset: 10,
	})
	if err != nil {
		t.Fatalf("Download() returned error: %v", err)
	}
	defer func() { _ = download.Close() }()

	if !download.Resumed {
		t.Error("Resumed = false on a 206; the caller would truncate and restart")
	}
	body, err := io.ReadAll(download.Body)
	if err != nil {
		t.Fatalf("reading body returned error: %v", err)
	}
	if string(body) != full[10:] {
		t.Errorf("body = %q, want the tail from offset 10", body)
	}
}

func TestDownloadReportsWhenARangeWasIgnored(t *testing.T) {
	t.Parallel()

	// A server that answers 200 to a range request is sending the whole file.
	// Appending that to a partial local file would duplicate everything already
	// written, which is why Resumed has to be checked rather than assumed.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("whole file"))
	}))
	t.Cleanup(server.Close)

	client, _ := New(Options{BaseURL: server.URL, Token: "t", Source: "files"})
	download, err := client.Download(context.Background(), DownloadRequest{
		Paths:  []string{"/a.txt"},
		Offset: 5,
	})
	if err != nil {
		t.Fatalf("Download() returned error: %v", err)
	}
	defer func() { _ = download.Close() }()

	if download.Resumed {
		t.Error("Resumed = true on a 200 response")
	}
}

func TestDownloadReadsTheSuggestedFilename(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="photos.tar.gz"`)
		_, _ = w.Write([]byte("data"))
	}))
	t.Cleanup(server.Close)

	client, _ := New(Options{BaseURL: server.URL, Token: "t", Source: "files"})
	download, err := client.Download(context.Background(), DownloadRequest{
		Paths:   []string{"/photos"},
		Archive: ArchiveTarGz,
	})
	if err != nil {
		t.Fatalf("Download() returned error: %v", err)
	}
	defer func() { _ = download.Close() }()

	if download.Filename != "photos.tar.gz" {
		t.Errorf("Filename = %q, want photos.tar.gz", download.Filename)
	}
}

func TestDownloadSurfacesAnErrorStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status":404,"message":"file does not exist"}`))
	}))
	t.Cleanup(server.Close)

	client, _ := New(Options{BaseURL: server.URL, Token: "t", Source: "files"})
	_, err := client.Download(context.Background(), DownloadRequest{Paths: []string{"/missing"}})
	if !IsNotFound(err) {
		t.Errorf("IsNotFound(%v) = false, want true", err)
	}
}
