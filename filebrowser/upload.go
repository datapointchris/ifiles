package filebrowser

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// Chunk offset and total size travel in headers, not in the body or the query
// string. The presence of the offset header is what puts the server on the
// chunked code path at all.
const (
	headerChunkOffset = "X-File-Chunk-Offset"
	headerTotalSize   = "X-File-Total-Size"
)

// statusGracefulPause is the non-standard code the server answers with when a
// chunk POST was aborted after a pause was registered. It means the partial
// upload survived and a later request can resume into it.
const statusGracefulPause = 499

// pauseTimeout bounds the pause request. The server holds a registered pause for
// one minute, so a pause that takes longer than a few seconds has already lost
// the race it was trying to win.
const pauseTimeout = 5 * time.Second

// UploadRequest describes a chunked upload.
type UploadRequest struct {
	// Path is the remote destination, including the filename.
	Path string
	// Size is the total size of the finished file. The server uses it to decide
	// when the last chunk has landed and the temp file can be moved into place, so
	// an inaccurate value either truncates the upload or leaves it unfinished
	// forever.
	Size int64
	// ChunkSize is the request body size for each chunk.
	ChunkSize int64
	// Offset resumes into a partial upload left by an earlier paused attempt.
	Offset int64
	// Override permits replacing an existing file. Without it the server answers
	// 409 on the first chunk.
	Override bool
	// Progress, when set, is called after each chunk with the total bytes sent.
	Progress func(sent int64)
}

// ErrUploadPaused reports that an upload was interrupted but its partial data
// survived on the server, so retrying with the same offset resumes rather than
// restarts.
var ErrUploadPaused = errors.New("upload paused; partial data kept on the server")

// ErrSourceTooShort reports that the reader ran out before Size was reached,
// which means the file changed underneath the transfer.
var ErrSourceTooShort = errors.New("source ended before the declared size")

// Upload sends a file as a sequence of chunked POSTs.
//
// Every upload is chunked, including small ones: a single chunk takes the same
// server code path, and the alternative — one unchunked request — cannot exceed
// the 100 MB body cap that Cloudflare's free tier imposes on this route. Chunking
// unconditionally means there is one path to get right rather than two.
//
// On cancellation the server is told to pause *before* the in-flight request is
// aborted, because that ordering is what decides whether the partial file is kept
// or deleted. See pauseUpload.
func (c *Client) Upload(ctx context.Context, source io.Reader, req UploadRequest) error {
	if req.Size < 0 {
		return fmt.Errorf("upload size is unknown")
	}
	if req.ChunkSize <= 0 {
		return fmt.Errorf("chunk size must be positive")
	}
	if req.Offset > req.Size {
		return fmt.Errorf("resume offset %d is past the end of a %d byte file", req.Offset, req.Size)
	}

	// Each chunk is read into this buffer before the request is issued, so the
	// declared Content-Length always matches the bytes actually sent. Streaming
	// straight from the reader is cheaper in memory, but a reader that ends early
	// then breaks the connection mid-body, and the server reads that as a failed
	// chunk and deletes the whole partial upload. One bounded buffer is a cheap
	// price for never destroying progress that way.
	bufferSize := req.ChunkSize
	if remaining := req.Size - req.Offset; remaining < bufferSize {
		bufferSize = remaining
	}
	buffer := make([]byte, bufferSize)

	sent := req.Offset
	// A zero-length file still needs one request, or nothing is ever created.
	for sent < req.Size || sent == req.Offset {
		length := req.Size - sent
		if length > req.ChunkSize {
			length = req.ChunkSize
		}

		chunk := buffer[:length]
		if _, err := io.ReadFull(source, chunk); err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
				return fmt.Errorf("%w: expected %d bytes, ran out at offset %d", ErrSourceTooShort, req.Size, sent)
			}
			return err
		}

		if err := c.putChunk(ctx, chunk, req, sent); err != nil {
			return err
		}
		sent += length

		if req.Progress != nil {
			req.Progress(sent)
		}
		if length == 0 {
			break
		}
	}
	return nil
}

// putChunk sends one chunk, translating a cancellation into a graceful pause.
func (c *Client) putChunk(ctx context.Context, chunk []byte, req UploadRequest, offset int64) error {
	query := c.query(req.Path)
	if req.Override {
		query.Set("override", "true")
	}

	// The chunk request runs on its own context so cancellation can be sequenced:
	// register the pause with the server first, then abort. Handing the user's
	// context straight to the request would tear the connection down before the
	// pause could be recorded, and the server would delete the partial file.
	chunkCtx, abort := context.WithCancel(context.WithoutCancel(ctx))
	defer abort()

	paused := make(chan struct{})
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			c.pauseUpload(req.Path)
			close(paused)
			abort()
		case <-done:
		}
	}()

	httpReq, err := c.newRequest(chunkCtx, http.MethodPost, "/resources", query, bytes.NewReader(chunk))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/octet-stream")
	httpReq.Header.Set(headerChunkOffset, strconv.FormatInt(offset, 10))
	httpReq.Header.Set(headerTotalSize, strconv.FormatInt(req.Size, 10))
	httpReq.ContentLength = int64(len(chunk))

	resp, err := c.http.Do(httpReq)
	if err != nil {
		select {
		case <-paused:
			return fmt.Errorf("%w: resume from offset %d", ErrUploadPaused, offset)
		default:
			return err
		}
	}
	defer func() { _ = resp.Body.Close() }()

	payload, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if readErr != nil {
		return readErr
	}

	switch {
	case resp.StatusCode == statusGracefulPause:
		return fmt.Errorf("%w: resume from offset %d", ErrUploadPaused, offset)
	case resp.StatusCode >= 400:
		return &APIError{
			Status:  resp.StatusCode,
			Method:  http.MethodPost,
			Path:    "/resources",
			Message: errorMessage(payload),
		}
	}
	return nil
}

// pauseUpload tells the server to keep the partial file if the in-flight chunk
// request now fails. It runs on a fresh context on purpose: the context that
// triggered the pause is already canceled, and using it would cancel this
// request too, which is the exact bug the pause exists to prevent.
//
// A failure here is deliberately swallowed. The upload is being abandoned either
// way, and the only consequence is that the resume starts from zero — reporting
// it would replace the real error with a less useful one.
func (c *Client) pauseUpload(remotePath string) {
	ctx, cancel := context.WithTimeout(context.Background(), pauseTimeout)
	defer cancel()
	_ = c.do(ctx, http.MethodPost, "/resources/pause", c.query(remotePath), nil, nil)
}
