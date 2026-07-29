package filebrowser

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
)

// ArchiveZip and ArchiveTarGz are the two formats the server can build. Anything
// else is rejected with a 500 rather than a 400, so the value is validated here.
const (
	ArchiveZip   = "zip"
	ArchiveTarGz = "tar.gz"
)

// DownloadRequest describes what to fetch.
type DownloadRequest struct {
	// Paths is one or more remote paths. A single file streams as itself; a
	// directory, or more than one path, streams as an archive the server builds —
	// there is no separate archive call to make first.
	Paths []string
	// Archive selects the format when the response will be an archive.
	Archive string
	// Offset resumes a partial transfer through an HTTP range request. Only a
	// single-file download can resume: the server serves those with
	// http.ServeContent, which honors Range, while an archive is generated on the
	// fly and has nothing to seek.
	Offset int64
}

// Download is an open response body plus what the client needs to write it out.
type Download struct {
	Body io.ReadCloser
	// Size is the number of bytes still to come, or -1 when the server did not
	// say — which is the normal case for an archive, since it is generated as it
	// streams.
	Size int64
	// Filename is the server's suggested name, from Content-Disposition. For an
	// archive it carries the correct extension for the format.
	Filename string
	// Resumed reports whether the server honored Offset. When false after a
	// non-zero Offset was requested, the local partial file has to be discarded
	// rather than appended to.
	Resumed bool
}

func (d *Download) Close() error { return d.Body.Close() }

// Download opens a stream for one or more remote paths.
//
// The route takes repeated ?file= parameters, not the ?path= that every other
// resource route uses. Getting that wrong yields a 400 that reads like a
// permissions problem.
func (c *Client) Download(ctx context.Context, req DownloadRequest) (*Download, error) {
	if len(req.Paths) == 0 {
		return nil, fmt.Errorf("no paths to download")
	}
	if req.Archive != "" && req.Archive != ArchiveZip && req.Archive != ArchiveTarGz {
		return nil, fmt.Errorf("archive format %q is not supported; use %s or %s", req.Archive, ArchiveZip, ArchiveTarGz)
	}

	query := url.Values{"source": []string{c.source}}
	for _, path := range req.Paths {
		query.Add("file", path)
	}
	if req.Archive != "" {
		query.Set("algo", req.Archive)
	}

	httpReq, err := c.newRequest(ctx, http.MethodGet, "/resources/download", query, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "*/*")
	if req.Offset > 0 {
		httpReq.Header.Set("Range", "bytes="+strconv.FormatInt(req.Offset, 10)+"-")
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		return nil, &APIError{
			Status:  resp.StatusCode,
			Method:  http.MethodGet,
			Path:    "/resources/download",
			Message: errorMessage(payload),
		}
	}

	return &Download{
		Body:     resp.Body,
		Size:     resp.ContentLength,
		Filename: attachmentFilename(resp.Header.Get("Content-Disposition")),
		Resumed:  resp.StatusCode == http.StatusPartialContent,
	}, nil
}

// attachmentFilename pulls the suggested name out of a Content-Disposition
// header, returning "" when the header is absent or unparsable — the caller
// always has the remote path to fall back on.
func attachmentFilename(header string) string {
	if header == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(header)
	if err != nil {
		return ""
	}
	return params["filename"]
}
