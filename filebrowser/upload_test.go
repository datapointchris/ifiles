package filebrowser

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

// chunkRecord is one observed chunk POST, in arrival order.
type chunkRecord struct {
	Offset int64
	Total  int64
	Body   []byte
	Query  map[string]string
}

// uploadServer reassembles chunks the way the real server does — seek to the
// offset, write, and treat offset+len >= total as completion — so a test can
// assert the file that would land on disk rather than just the requests made.
type uploadServer struct {
	mu        sync.Mutex
	chunks    []chunkRecord
	assembled []byte
	completed bool
	paused    bool
	// pauseSeenBeforeAbort records whether /resources/pause arrived before the
	// chunk POST failed, which is the ordering the whole resume feature rests on.
	pauseSeenBeforeAbort bool
	inFlightChunk        bool
	bodyDelay            time.Duration
}

func (u *uploadServer) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/resources/pause", func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		u.paused = true
		if u.inFlightChunk {
			u.pauseSeenBeforeAbort = true
		}
		u.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("POST /api/resources", func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.ParseInt(r.Header.Get(headerChunkOffset), 10, 64)
		total, _ := strconv.ParseInt(r.Header.Get(headerTotalSize), 10, 64)

		u.mu.Lock()
		u.inFlightChunk = true
		delay := u.bodyDelay
		u.mu.Unlock()

		// Holding the request open is how a test arranges for a cancellation to
		// land while a chunk POST is genuinely in flight.
		if delay > 0 {
			time.Sleep(delay)
		}

		body, readErr := io.ReadAll(r.Body)

		u.mu.Lock()
		defer u.mu.Unlock()
		u.inFlightChunk = false

		if readErr != nil {
			// Mirrors the server: truncate the incomplete chunk, then keep or drop
			// the partial file depending on whether a pause was registered.
			if int64(len(u.assembled)) > offset {
				u.assembled = u.assembled[:offset]
			}
			if u.paused {
				w.WriteHeader(statusGracefulPause)
				return
			}
			u.assembled = nil
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		query := map[string]string{}
		for key := range r.URL.Query() {
			query[key] = r.URL.Query().Get(key)
		}
		u.chunks = append(u.chunks, chunkRecord{Offset: offset, Total: total, Body: body, Query: query})

		for int64(len(u.assembled)) < offset {
			u.assembled = append(u.assembled, 0)
		}
		u.assembled = append(u.assembled[:offset], body...)

		if offset+int64(len(body)) >= total {
			u.completed = true
		}
		w.WriteHeader(http.StatusOK)
	})

	return mux
}

func newUploadTest(t *testing.T) (*Client, *uploadServer) {
	t.Helper()

	backend := &uploadServer{}
	server := httptest.NewServer(backend.handler())
	t.Cleanup(server.Close)

	client, err := New(Options{BaseURL: server.URL, Token: "test-token", Source: "files"})
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}
	return client, backend
}

func TestUploadSplitsIntoChunksAtTheRequestedSize(t *testing.T) {
	t.Parallel()

	client, backend := newUploadTest(t)
	payload := bytes.Repeat([]byte("abcdefghij"), 10) // 100 bytes

	err := client.Upload(context.Background(), bytes.NewReader(payload), UploadRequest{
		Path:      "/dest.bin",
		Size:      int64(len(payload)),
		ChunkSize: 30,
	})
	if err != nil {
		t.Fatalf("Upload() returned error: %v", err)
	}

	wantOffsets := []int64{0, 30, 60, 90}
	if len(backend.chunks) != len(wantOffsets) {
		t.Fatalf("sent %d chunks, want %d", len(backend.chunks), len(wantOffsets))
	}
	for i, want := range wantOffsets {
		if backend.chunks[i].Offset != want {
			t.Errorf("chunk %d offset = %d, want %d", i, backend.chunks[i].Offset, want)
		}
		if backend.chunks[i].Total != int64(len(payload)) {
			t.Errorf("chunk %d total = %d, want %d on every chunk", i, backend.chunks[i].Total, len(payload))
		}
	}
	if !backend.completed {
		t.Error("server never saw the final chunk; the upload would stay a temp file forever")
	}
	if !bytes.Equal(backend.assembled, payload) {
		t.Error("reassembled bytes differ from the source")
	}
}

func TestUploadSendsPathAndSourceOnEveryChunk(t *testing.T) {
	t.Parallel()

	client, backend := newUploadTest(t)
	payload := bytes.Repeat([]byte("x"), 50)

	if err := client.Upload(context.Background(), bytes.NewReader(payload), UploadRequest{
		Path:      "/photos/holiday snap #1.jpg",
		Size:      int64(len(payload)),
		ChunkSize: 20,
	}); err != nil {
		t.Fatalf("Upload() returned error: %v", err)
	}

	for i, chunk := range backend.chunks {
		// A path with a space and a # survives only if it is query-encoded; sent
		// raw, the # truncates it into a fragment and the file lands under the
		// wrong name.
		if chunk.Query["path"] != "/photos/holiday snap #1.jpg" {
			t.Errorf("chunk %d path = %q, want the full path decoded intact", i, chunk.Query["path"])
		}
		if chunk.Query["source"] != "files" {
			t.Errorf("chunk %d source = %q, want files", i, chunk.Query["source"])
		}
	}
}

func TestUploadZeroLengthFileStillCreatesIt(t *testing.T) {
	t.Parallel()

	client, backend := newUploadTest(t)

	if err := client.Upload(context.Background(), bytes.NewReader(nil), UploadRequest{
		Path:      "/empty",
		Size:      0,
		ChunkSize: 1024,
	}); err != nil {
		t.Fatalf("Upload() returned error: %v", err)
	}

	// Without one request an empty file is never created at all, and the loop
	// condition that allows it is easy to lose in a refactor.
	if len(backend.chunks) != 1 {
		t.Fatalf("sent %d chunks for an empty file, want exactly 1", len(backend.chunks))
	}
	if !backend.completed {
		t.Error("server did not consider the empty upload complete")
	}
}

func TestUploadResumeProducesAByteIdenticalFile(t *testing.T) {
	t.Parallel()

	client, backend := newUploadTest(t)
	payload := bytes.Repeat([]byte("0123456789"), 10) // 100 bytes

	// First attempt has only the leading 40 bytes available, which is what a file
	// truncated underneath a transfer looks like.
	err := client.Upload(context.Background(), bytes.NewReader(payload[:40]), UploadRequest{
		Path:      "/dest.bin",
		Size:      100,
		ChunkSize: 20,
		Offset:    0,
	})
	if !errors.Is(err, ErrSourceTooShort) {
		t.Fatalf("first attempt error = %v, want ErrSourceTooShort", err)
	}
	if got := len(backend.assembled); got != 40 {
		t.Fatalf("server holds %d bytes after the short read, want the 40 already sent kept intact", got)
	}

	// Resume: seek the source and continue from where the server got to.
	if err := client.Upload(context.Background(), bytes.NewReader(payload[40:]), UploadRequest{
		Path:      "/dest.bin",
		Size:      100,
		ChunkSize: 20,
		Offset:    40,
		Override:  true,
	}); err != nil {
		t.Fatalf("resumed Upload() returned error: %v", err)
	}

	if !backend.completed {
		t.Error("resumed upload never completed")
	}
	if !bytes.Equal(backend.assembled, payload) {
		t.Errorf("resumed file differs from the source: got %d bytes", len(backend.assembled))
	}
}

func TestUploadResumeStartsAtTheGivenOffset(t *testing.T) {
	t.Parallel()

	client, backend := newUploadTest(t)
	payload := bytes.Repeat([]byte("z"), 100)

	if err := client.Upload(context.Background(), bytes.NewReader(payload[60:]), UploadRequest{
		Path:      "/dest.bin",
		Size:      100,
		ChunkSize: 20,
		Offset:    60,
	}); err != nil {
		t.Fatalf("Upload() returned error: %v", err)
	}

	if backend.chunks[0].Offset != 60 {
		t.Errorf("first chunk offset = %d, want 60; a resume that restarts at 0 duplicates the transfer", backend.chunks[0].Offset)
	}
	wantOffsets := []int64{60, 80}
	if len(backend.chunks) != len(wantOffsets) {
		t.Fatalf("sent %d chunks, want %d", len(backend.chunks), len(wantOffsets))
	}
}

func TestUploadRejectsAnOffsetPastTheEnd(t *testing.T) {
	t.Parallel()

	client, _ := newUploadTest(t)
	err := client.Upload(context.Background(), bytes.NewReader(nil), UploadRequest{
		Path:      "/dest.bin",
		Size:      10,
		ChunkSize: 5,
		Offset:    20,
	})
	if err == nil {
		t.Fatal("Upload() with an out-of-range offset succeeded, want an error")
	}
}

func TestUploadOverrideIsSentOnlyWhenAsked(t *testing.T) {
	t.Parallel()

	client, backend := newUploadTest(t)
	payload := bytes.Repeat([]byte("y"), 10)

	if err := client.Upload(context.Background(), bytes.NewReader(payload), UploadRequest{
		Path:      "/dest.bin",
		Size:      int64(len(payload)),
		ChunkSize: 100,
		Override:  true,
	}); err != nil {
		t.Fatalf("Upload() returned error: %v", err)
	}
	if backend.chunks[0].Query["override"] != "true" {
		t.Error("override=true was not sent; the server would answer 409 on an existing file")
	}
}

func TestUploadConflictIsDistinguishable(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The real server returns a bare 409 with no body for this case, which is
		// why the error type cannot rely on a message being present.
		w.WriteHeader(http.StatusConflict)
	}))
	t.Cleanup(server.Close)

	client, err := New(Options{BaseURL: server.URL, Token: "t", Source: "files"})
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	err = client.Upload(context.Background(), bytes.NewReader([]byte("data")), UploadRequest{
		Path:      "/exists",
		Size:      4,
		ChunkSize: 100,
	})
	if !IsConflict(err) {
		t.Errorf("IsConflict(%v) = false, want true", err)
	}
}

func TestUploadPausesBeforeAbortingOnCancellation(t *testing.T) {
	t.Parallel()

	client, backend := newUploadTest(t)
	backend.bodyDelay = 2 * time.Second

	payload := bytes.Repeat([]byte("a"), 4096)
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.Upload(ctx, bytes.NewReader(payload), UploadRequest{
			Path:      "/dest.bin",
			Size:      int64(len(payload)),
			ChunkSize: int64(len(payload)),
		})
	}()

	// Long enough for the request to reach the server, short enough that it is
	// still being held there.
	time.Sleep(300 * time.Millisecond)
	cancel()

	var err error
	select {
	case err = <-errCh:
	case <-time.After(10 * time.Second):
		t.Fatal("Upload() did not return after cancellation")
	}

	if !errors.Is(err, ErrUploadPaused) {
		t.Fatalf("Upload() error = %v, want ErrUploadPaused", err)
	}

	backend.mu.Lock()
	defer backend.mu.Unlock()
	if !backend.paused {
		t.Fatal("server never received the pause request; the partial upload would be deleted")
	}
	// This is the ordering the design hangs on: the pause has to be registered
	// while the chunk POST is still open, because the server only consults the
	// pause cache when that request fails.
	if !backend.pauseSeenBeforeAbort {
		t.Error("pause arrived after the chunk request ended, so the server would not have kept the partial file")
	}
}
