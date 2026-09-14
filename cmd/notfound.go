package cmd

import (
	"errors"

	"github.com/datapointchris/goclikit"

	"github.com/datapointchris/ifiles/filebrowser"
)

// notFound is the classifier [goclikit.WithNotFound] calls: the server's
// not-found, and the line naming the path that was absent.
//
// The subject comes out of the error rather than out of the command's
// arguments, because a verb can touch two paths. `ifiles move a b` stats the
// source before writing the destination, and a subject composed here from argv
// would have to guess which of the two the server refused.
//
// This API cannot supply the subject itself. FileBrowser writes a JSON body
// only when a handler returns an error value, so a 404 usually arrives bare —
// which is why [filebrowser.APIError] carries the requested path and this reads
// it from there rather than from the message.
//
// An empty Resource returns false, leaving the error alone. That is the
// download of several files at once, where no single path is the subject, and
// the health check, which is about no resource at all.
func notFound(err error) (string, bool) {
	var apiErr *filebrowser.APIError
	if !errors.As(err, &apiErr) || !filebrowser.IsNotFound(err) || apiErr.Resource == "" {
		return "", false
	}
	return apiErr.Resource + " does not exist on the server", true
}

// recoveryHints are the commands a not-found names, recorded on the root.
//
// One set covers the whole tree here, which is unlike icb. Every ifiles noun is
// a path, and the way back from any missing one is to list the directory it
// should have been in — so a per-verb hint would be the same sentence repeated
// at every command. goclikit takes the nearest ancestor carrying hints, so a
// verb that ever needs a different way back overrides this by declaring its own.
var recoveryHints = []string{
	"List the directory it should be in: ifiles list <parent>",
	"Search for it by name: ifiles search <name>",
}

func init() {
	goclikit.WithRecoveryHints(rootCmd, recoveryHints...)
}
