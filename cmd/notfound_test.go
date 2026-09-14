package cmd

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/datapointchris/goclikit"
	"github.com/spf13/cobra"

	"github.com/datapointchris/ifiles/filebrowser"
)

func TestNotFoundNamesTheRequestedPath(t *testing.T) {
	t.Parallel()

	err := &filebrowser.APIError{
		Status:   http.StatusNotFound,
		Method:   http.MethodGet,
		Path:     "/resources",
		Resource: "/photos/missing.jpg",
	}

	subject, ok := notFound(err)
	if !ok {
		t.Fatal("notFound() = false, want true for a 404 carrying a resource")
	}
	if !strings.Contains(subject, "/photos/missing.jpg") {
		t.Errorf("subject = %q, want the path the caller asked for", subject)
	}
	// The route is every resource call's, so a subject naming it would be the
	// same sentence for every missing file.
	if strings.Contains(subject, "/resources") {
		t.Errorf("subject = %q, names the route rather than the resource", subject)
	}
}

func TestNotFoundDeclinesWhenNoSingleResourceIsTheSubject(t *testing.T) {
	t.Parallel()

	// A multi-file download and the health check both leave Resource empty.
	// goclikit reads an empty subject as false, so the error is left alone
	// rather than rewritten into a claim nothing established.
	err := &filebrowser.APIError{Status: http.StatusNotFound, Path: "/resources/download"}
	if _, ok := notFound(err); ok {
		t.Error("notFound() = true for a 404 naming no resource")
	}
}

func TestNotFoundDeclinesAnythingThatIsNotA404(t *testing.T) {
	t.Parallel()

	conflict := &filebrowser.APIError{Status: http.StatusConflict, Resource: "/exists.txt"}
	if _, ok := notFound(conflict); ok {
		t.Error("notFound() = true for a 409")
	}
	if _, ok := notFound(errors.New("connection refused")); ok {
		t.Error("notFound() = true for a transport error")
	}
}

func TestEveryPathTakingCommandInheritsARecoveryHint(t *testing.T) {
	t.Parallel()

	// goclikit walks to the nearest ancestor carrying hints, so a hint on the
	// root reaches every command that does not declare its own. A verb whose
	// way back is genuinely different overrides it there.
	reaches := func(cmd *cobra.Command) bool {
		for current := cmd; current != nil; current = current.Parent() {
			if current.Annotations[goclikit.RecoveryHintsAnnotation] != "" {
				return true
			}
		}
		return false
	}

	for _, name := range []string{"list", "read", "move", "copy", "mkdir", "download", "search"} {
		command, _, err := rootCmd.Find([]string{name})
		if err != nil {
			t.Fatalf("Find(%q) = %v", name, err)
		}
		if !reaches(command) {
			t.Errorf("%s reaches no recovery hints", name)
		}
	}
}
