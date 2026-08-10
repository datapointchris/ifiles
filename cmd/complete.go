package cmd

import (
	"context"
	"path"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/datapointchris/ifiles/v2/completion"
	"github.com/datapointchris/ifiles/v2/config"
	"github.com/datapointchris/ifiles/v2/filebrowser"
)

// completionTimeout bounds a listing fetched to answer a Tab. It is deliberately
// not --timeout, which exists to let a multi-gigabyte transfer run unbounded: a
// completion that takes even a few seconds reads as a hung shell, and the right
// answer at that point is no suggestions rather than a late one.
const completionTimeout = 2 * time.Second

// entryFilter says whether a position accepts any remote entry or only a
// directory, which is the only thing that differs between the two completers.
type entryFilter bool

const (
	allEntries      entryFilter = false
	directoriesOnly entryFilter = true
)

// completeRemotePath completes the single remote path of a command that takes one.
func completeRemotePath(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeRemote(cmd, toComplete, allEntries)
}

// completeRemoteDirectory completes a position where a file would be rejected:
// `list` on a file is an error, and `mkdir` names one that does not exist yet, so
// what is being completed there is its parent.
func completeRemoteDirectory(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeRemote(cmd, toComplete, directoriesOnly)
}

// completeTwoRemotePaths completes both sides of move and copy, which never touch the
// local filesystem.
func completeTwoRemotePaths(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 1 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeRemote(cmd, toComplete, allEntries)
}

// completeRemoteThenLocal completes `download <remote> [local]`: the remote path from
// the server, and the local destination by handing the position back to the shell.
func completeRemoteThenLocal(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		return completeRemote(cmd, toComplete, allEntries)
	case 1:
		return nil, cobra.ShellCompDirectiveDefault
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeLocalThenRemote completes `upload <local> [remote]`, the one command whose
// argument order puts the local path first.
func completeLocalThenRemote(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		return nil, cobra.ShellCompDirectiveDefault
	case 1:
		// Any entry, not only directories: the destination may name the remote file
		// itself, which is what `upload draft.md /docs/final.md` relies on.
		return completeRemote(cmd, toComplete, allEntries)
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

func completeRemote(cmd *cobra.Command, toComplete string, filter entryFilter) ([]string, cobra.ShellCompDirective) {
	typedDir, _ := path.Split(toComplete)
	items, err := completionListing(cmd, filebrowser.CleanPath(typedDir))
	if err != nil {
		// Stdout is the candidate list, so there is nowhere to report this: a message
		// written there arrives as a suggestion, and a returned error string lands in
		// the user's command line. Not logged in, tunnel down, expired token, a path
		// that is not a directory — all of it has to look like "no matches".
		return nil, cobra.ShellCompDirectiveError
	}
	return completionCandidates(toComplete, items, filter)
}

// completionCandidates filters a listing to the entries that could finish the word
// being typed.
func completionCandidates(toComplete string, items []filebrowser.Item, filter entryFilter) ([]string, cobra.ShellCompDirective) {
	// The directory half is kept exactly as it was typed rather than normalized. A
	// candidate has to start with the word it replaces — bash filters the returned
	// list through `compgen -W ... -- "$cur"` and zsh through _describe — so
	// answering `/photos/x` to a typed `photos/` would silently match nothing.
	typedDir, prefix := path.Split(toComplete)

	// A shell hides dotfiles until they are asked for by name, and a listing whose
	// visible entry is outnumbered by dotfiles is no more useful here than there.
	includeHidden := strings.HasPrefix(prefix, ".")

	candidates := make([]string, 0, len(items))
	onlyDirectories := true
	for _, item := range items {
		if filter == directoriesOnly && !item.IsDir() {
			continue
		}
		if item.Hidden && !includeHidden {
			continue
		}
		if !strings.HasPrefix(item.Name, prefix) {
			continue
		}

		candidate := typedDir + item.Name
		if item.IsDir() {
			// The trailing slash is what makes a directory continuable: pressing Tab
			// again descends instead of re-offering the directory itself.
			candidate += "/"
		} else {
			onlyDirectories = false
		}
		candidates = append(candidates, candidate+"\t"+completionDescription(item))
	}

	// Without this the shell falls back to completing the working directory, which
	// offers local names that cannot exist on the server — and does so exactly when
	// completion failed, which is when a wrong suggestion is least wanted.
	directive := cobra.ShellCompDirectiveNoFileComp
	if len(candidates) == 0 {
		return nil, directive
	}
	// A unique match is the only case where the shell appends a space on its own,
	// and a space after `/photos/` has to be backspaced before descending. The
	// directive is per-call rather than per-candidate, but the filtered set narrows
	// to the single entry being accepted, so this is only set when that entry is a
	// directory.
	if onlyDirectories {
		directive |= cobra.ShellCompDirectiveNoSpace
	}
	return candidates, directive
}

// completionDescription is the text zsh and fish show beside a candidate. A size
// is what distinguishes two similarly-named files, which is the situation that
// sends someone to completion in the first place.
func completionDescription(item filebrowser.Item) string {
	if item.IsDir() {
		return "dir"
	}
	return config.FormatSize(item.Size)
}

// completionListing returns a directory's entries, from the cache when it holds a
// fresh copy.
func completionListing(cmd *cobra.Command, remotePath string) ([]filebrowser.Item, error) {
	client, cfg, err := newClient()
	if err != nil {
		return nil, err
	}

	cache := completion.NewCache()
	key := completion.Key(cfg.URL, client.Source(), remotePath)
	if items, ok := cache.Lookup(key); ok {
		return items, nil
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), completionTimeout)
	defer cancel()

	listing, err := client.List(ctx, remotePath)
	if err != nil {
		return nil, err
	}

	items := listing.Entries()
	// A cache that cannot be written costs a request per Tab and nothing else.
	_ = cache.Store(key, items)
	return items, nil
}

// completeSources completes --source from the server's own list, so the flag that
// every resource route requires does not have to be remembered.
func completeSources(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	client, _, err := newClient()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), completionTimeout)
	defer cancel()

	sources, err := client.Sources(ctx)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	names := make([]string, 0, len(sources))
	for _, source := range sources {
		if strings.HasPrefix(source.Name, toComplete) {
			names = append(names, source.Name)
		}
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeShareHashes completes `shares delete` from the account's live links.
//
// Uncached, unlike the path completers. A share list is a handful of rows rather
// than a directory of thousands, and the one thing a stale answer here would do
// is offer a hash that was revoked in the previous command — which is precisely
// the sequence someone completing a revocation is in the middle of.
func completeShareHashes(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	client, _, err := newClient()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), completionTimeout)
	defer cancel()

	shares, err := client.Shares(ctx)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	candidates := make([]string, 0, len(shares))
	for _, share := range shares {
		if !strings.HasPrefix(share.Hash, toComplete) {
			continue
		}
		// The path is the description because the hash is unreadable by design:
		// what it points at is the only way to tell two of them apart.
		candidates = append(candidates, share.Hash+"\t"+share.Path)
	}
	return candidates, cobra.ShellCompDirectiveNoFileComp
}

// mustCompleteFlag attaches a completion function to a flag, panicking if that
// flag does not exist. Cobra reports the mismatch as an error a caller is free to
// discard, which turns a renamed flag into a completion that silently stops
// working — indistinguishable, at the prompt, from a server that is down. The
// panic fires unconditionally on the first run of the binary and on `go test`, so
// it cannot reach a release.
//
// It is called from the init of whichever file defines the flag, not from this
// one: init functions run in filename order, so anything registered here would
// run before get.go and put.go had created the flags to register against.
func mustCompleteFlag(cmd *cobra.Command, flag string, fn cobra.CompletionFunc) {
	if err := cmd.RegisterFlagCompletionFunc(flag, fn); err != nil {
		panic(err)
	}
}

func init() {
	// Assigning the argument completers is safe here whatever the init order,
	// because every command variable is fully constructed before any init runs.
	// One place for the whole positional surface means what completes where can be
	// read without opening eight files.
	downloadCmd.ValidArgsFunction = completeRemoteThenLocal
	uploadCmd.ValidArgsFunction = completeLocalThenRemote
	readCmd.ValidArgsFunction = completeRemotePath
	deleteCmd.ValidArgsFunction = completeRemotePath
	moveCmd.ValidArgsFunction = completeTwoRemotePaths
	copyCmd.ValidArgsFunction = completeTwoRemotePaths
	listCmd.ValidArgsFunction = completeRemoteDirectory
	mkdirCmd.ValidArgsFunction = completeRemoteDirectory
	sharesCreateCmd.ValidArgsFunction = completeRemotePath
	sharesListCmd.ValidArgsFunction = completeRemotePath
	sharesDeleteCmd.ValidArgsFunction = completeShareHashes

	rootCmd.SetCompletionCommandGroupID(groupTool)
}
