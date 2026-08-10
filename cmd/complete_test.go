package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/datapointchris/ifiles/v2/filebrowser"
)

// sampleListing is one directory holding a folder, two files, and a dotfile —
// enough for every rule the candidate builder applies.
func sampleListing() []filebrowser.Item {
	return []filebrowser.Item{
		{Name: "raw", Type: filebrowser.DirectoryType},
		{Name: "raw.cr2", Type: "image/x-canon-cr2", Size: 25 << 20},
		{Name: "sunset.jpg", Type: "image/jpeg", Size: 4 << 20},
		{Name: ".thumbnails", Type: filebrowser.DirectoryType, Hidden: true},
	}
}

// candidateNames drops the tab-separated descriptions, which are display text
// rather than part of the word the shell substitutes.
func candidateNames(candidates []string) []string {
	names := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		names = append(names, strings.SplitN(candidate, "\t", 2)[0])
	}
	return names
}

func TestCompletionKeepsTheDirectoryExactlyAsTyped(t *testing.T) {
	t.Parallel()

	// The word is relative, which CleanPath would happily absolutize. A candidate
	// must still start with what was typed: bash filters the returned list through
	// `compgen -W ... -- "$cur"` and zsh through _describe, so answering
	// /photos/raw.cr2 to a typed photos/ra matches nothing and Tab appears broken.
	candidates, _ := completionCandidates("photos/ra", sampleListing(), allEntries)

	got := candidateNames(candidates)
	want := []string{"photos/raw/", "photos/raw.cr2"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("candidates = %v, want %v", got, want)
	}
}

func TestCompletionMarksADirectoryAndSuppressesTheSpace(t *testing.T) {
	t.Parallel()

	candidates, directive := completionCandidates("/photos/raw/", sampleListing(), directoriesOnly)

	// The trailing slash is what makes a second Tab descend rather than re-offer
	// the directory, and the space zsh would otherwise append after a unique match
	// has to be backspaced before it can.
	if got := candidateNames(candidates); len(got) != 1 || got[0] != "/photos/raw/raw/" {
		t.Fatalf("candidates = %v, want [/photos/raw/raw/]", got)
	}
	if directive&cobra.ShellCompDirectiveNoSpace == 0 {
		t.Error("a directory completion did not set NoSpace")
	}
}

func TestCompletionAllowsASpaceAfterAFile(t *testing.T) {
	t.Parallel()

	// A file is the end of the argument, so suppressing the space here would make
	// every download need a manual space before the next word.
	candidates, directive := completionCandidates("/photos/sun", sampleListing(), allEntries)

	if got := candidateNames(candidates); len(got) != 1 || got[0] != "/photos/sunset.jpg" {
		t.Fatalf("candidates = %v, want [/photos/sunset.jpg]", got)
	}
	if directive&cobra.ShellCompDirectiveNoSpace != 0 {
		t.Error("a unique file completion set NoSpace")
	}
}

func TestCompletionNeverFallsBackToLocalFiles(t *testing.T) {
	t.Parallel()

	// Remote paths have nothing to do with the working directory. Without
	// NoFileComp the shell offers local names — and does it precisely when the
	// remote listing came back empty, which is when a wrong suggestion is worst.
	for _, word := range []string{"/photos/sun", "/photos/nothing-matches-this"} {
		_, directive := completionCandidates(word, sampleListing(), allEntries)
		if directive&cobra.ShellCompDirectiveNoFileComp == 0 {
			t.Errorf("completing %q did not set NoFileComp", word)
		}
	}
}

func TestCompletionHidesDotfilesUntilAskedForByName(t *testing.T) {
	t.Parallel()

	candidates, _ := completionCandidates("/photos/", sampleListing(), allEntries)
	for _, name := range candidateNames(candidates) {
		if strings.HasPrefix(path.Base(name), ".") {
			t.Errorf("candidate %q is hidden and was offered without a leading dot", name)
		}
	}

	candidates, _ = completionCandidates("/photos/.", sampleListing(), allEntries)
	if got := candidateNames(candidates); len(got) != 1 || got[0] != "/photos/.thumbnails/" {
		t.Errorf("candidates = %v, want [/photos/.thumbnails/]", got)
	}
}

func TestCompletionForADirectoryOnlyPositionOmitsFiles(t *testing.T) {
	t.Parallel()

	// `ls` on a file is an error and `mkdir` names a directory that does not exist
	// yet, so offering a file in either position can only produce a failed command.
	candidates, _ := completionCandidates("/photos/", sampleListing(), directoriesOnly)

	if got := candidateNames(candidates); len(got) != 1 || got[0] != "/photos/raw/" {
		t.Errorf("candidates = %v, want [/photos/raw/]", got)
	}
}

func TestCompletionDescribesAnEntryBySize(t *testing.T) {
	t.Parallel()

	candidates, _ := completionCandidates("/photos/", sampleListing(), allEntries)

	descriptions := map[string]string{}
	for _, candidate := range candidates {
		parts := strings.SplitN(candidate, "\t", 2)
		if len(parts) != 2 {
			t.Fatalf("candidate %q carries no description", candidate)
		}
		descriptions[parts[0]] = parts[1]
	}
	// The size is the whole point of the description: two files whose names differ
	// in one character are exactly the case that sends someone to completion.
	if got := descriptions["/photos/raw.cr2"]; got != "25.0M" {
		t.Errorf("description for raw.cr2 = %q, want 25.0M", got)
	}
	if got := descriptions["/photos/raw/"]; got != "dir" {
		t.Errorf("description for the directory = %q, want dir", got)
	}
}

// The protocol between the shell and this binary is a hidden `__complete`
// subcommand: candidate lines on stdout, then a colon-prefixed directive. This
// runs the whole path — config, token, request, cache — because the pieces above
// can all be right while the wiring that reaches them is not.
func TestCompleteSubcommandListsRemotePathsOverTheAPI(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") == "" {
			t.Error("completion request carried no Authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"path": "/", "name": "/", "type": "directory",
			"folders": [{"name": "photos", "type": "directory"}],
			"files": [{"name": "photo-of-a-cat.jpg", "type": "image/jpeg", "size": 2048}]
		}`))
	}))
	defer server.Close()

	output := completeThroughRootCmd(t, server.URL, "__complete", "download", "/pho")

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		t.Fatalf("__complete produced %q, want candidates and a directive", output)
	}
	if !strings.HasPrefix(lines[0], "/photos/\t") {
		t.Errorf("first candidate = %q, want /photos/ with a description", lines[0])
	}
	if !strings.HasPrefix(lines[1], "/photo-of-a-cat.jpg\t") {
		t.Errorf("second candidate = %q, want /photo-of-a-cat.jpg with a description", lines[1])
	}
	// Cobra's own line, and the marker that the shell script parses.
	if last := lines[len(lines)-1]; !strings.HasPrefix(last, ":") {
		t.Errorf("last line = %q, want a :directive line", last)
	}
	if requests != 1 {
		t.Errorf("completion made %d requests, want 1", requests)
	}
}

// A completion has nowhere to report a failure: stdout is the candidate list, so
// anything written there is offered as a suggestion, and cobra puts a returned
// error into the shell's buffer.
func TestCompleteSubcommandStaysSilentWhenTheServerFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status": 401, "message": "token expired"}`))
	}))
	defer server.Close()

	output := completeThroughRootCmd(t, server.URL, "__complete", "download", "/pho")

	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		t.Errorf("a failed completion offered %q as a candidate", line)
	}
	if !strings.Contains(output, ":1") {
		t.Errorf("output %q does not carry the error directive", output)
	}
}

// completeThroughRootCmd runs the real command tree against a fake server, with
// every path it touches redirected into the test's temp directories.
func completeThroughRootCmd(t *testing.T, serverURL string, args ...string) string {
	t.Helper()

	// XDG_CACHE_HOME first: without it the listing cache is written to the user's
	// real one, where it would then answer the next completion of the same path.
	cacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	t.Setenv("IFILES_URL", serverURL)
	t.Setenv("IFILES_SOURCE", "files")
	// The env token wins over both stores, so no keychain is touched.
	t.Setenv("IFILES_TOKEN", "test-token")

	empty := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	previous := configPath
	configPath = empty
	t.Cleanup(func() { configPath = previous })

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})

	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("__complete returned error: %v", err)
	}
	return out.String()
}
