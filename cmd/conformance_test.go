package cmd

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/datapointchris/goclikit"
	"github.com/datapointchris/goselfupdate/autoupdate"
	"github.com/spf13/cobra"
)

// These tests state the CLI's surface rules once, by walking the command tree
// or the source, so a command added later is covered by construction rather
// than by someone remembering to extend a list.

// availableLeaves walks the tree for every runnable command a user can reach.
// Hidden commands such as cobra's __complete are not part of the surface.
func availableLeaves(root *cobra.Command) []*cobra.Command {
	var leaves []*cobra.Command
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		for _, child := range c.Commands() {
			if !child.IsAvailableCommand() {
				continue
			}
			if child.Runnable() && !child.HasAvailableSubCommands() {
				leaves = append(leaves, child)
			}
			walk(child)
		}
	}
	walk(root)
	return leaves
}

// Root help prints commands by group, and a command with no group falls into
// cobra's "Additional Commands" block below them, where nobody looks for a
// verb. A subcommand inherits its place from the top-level command it sits
// under, so only the top level carries a GroupID.
func TestEveryTopLevelCommandIsInADeclaredGroup(t *testing.T) {
	declared := map[string]bool{}
	for _, group := range rootCmd.Groups() {
		declared[group.ID] = true
	}
	for _, command := range rootCmd.Commands() {
		if !command.IsAvailableCommand() {
			continue
		}
		if !declared[command.GroupID] {
			t.Errorf("%q has GroupID %q, want one of the root's declared groups", command.Name(), command.GroupID)
		}
	}
}

// A command that caps its rows with --limit is a listing, and every listing
// offers --json, so a script never has to parse the table a person reads.
func TestEveryListingOffersJSON(t *testing.T) {
	for _, leaf := range availableLeaves(rootCmd) {
		if leaf.Flags().Lookup("limit") != nil && leaf.Flags().Lookup("json") == nil {
			t.Errorf("%q takes --limit but not --json", leaf.CommandPath())
		}
	}
}

// A caller piping `ifiles ... --json` somewhere that gets the command line wrong
// must see the mistake on stderr and nothing on stdout, or the half-written
// stdout is parsed as data.
func TestAUsageErrorLeavesStdoutEmpty(t *testing.T) {
	for _, leaf := range availableLeaves(rootCmd) {
		path := strings.Fields(leaf.CommandPath())[1:]
		t.Run(strings.Join(path, " "), func(t *testing.T) {
			var stdout bytes.Buffer
			original := os.Args
			os.Args = append(append([]string{"ifiles"}, path...), "--no-such-flag")
			rootCmd.SetOut(&stdout)
			rootCmd.SetErr(&bytes.Buffer{})
			t.Cleanup(func() {
				os.Args = original
				rootCmd.SetOut(nil)
				rootCmd.SetErr(nil)
			})

			err := execute(t.Context(), autoupdate.Config{Suppress: true})
			if !errors.Is(err, goclikit.ErrUsage) {
				t.Errorf("an unknown flag returned %v, want a usage error", err)
			}
			if stdout.Len() != 0 {
				t.Errorf("a usage error wrote %d bytes to stdout, want none", stdout.Len())
			}
		})
	}
}

// Every write picks its stream through cmd.OutOrStdout or cmd.ErrOrStderr.
// fmt.Print*, the print builtins and a bare os.Stdout pick stdout implicitly,
// which is how a diagnostic ends up inside --json output or inside the
// completion candidates a shell reads from stdout.
func TestNoWriteChoosesStdoutImplicitly(t *testing.T) {
	moduleRoot := ".."
	fset := token.NewFileSet()
	parsed := 0
	err := filepath.WalkDir(moduleRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); name != ".." && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		parsed++
		ast.Inspect(file, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.SelectorExpr:
				if pkg, ok := n.X.(*ast.Ident); ok && pkg.Name == "os" && n.Sel.Name == "Stdout" {
					t.Errorf("%s: os.Stdout, want cmd.OutOrStdout()", fset.Position(n.Pos()))
				}
				if pkg, ok := n.X.(*ast.Ident); ok && pkg.Name == "fmt" && strings.HasPrefix(n.Sel.Name, "Print") {
					t.Errorf("%s: fmt.%s writes to stdout implicitly", fset.Position(n.Pos()), n.Sel.Name)
				}
			case *ast.CallExpr:
				if fn, ok := n.Fun.(*ast.Ident); ok && (fn.Name == "print" || fn.Name == "println") {
					t.Errorf("%s: the %s builtin writes outside both streams", fset.Position(n.Pos()), fn.Name)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", moduleRoot, err)
	}
	// A walk that found no source would pass every check above.
	if parsed == 0 {
		t.Fatalf("parsed no Go files under %s", moduleRoot)
	}
}
