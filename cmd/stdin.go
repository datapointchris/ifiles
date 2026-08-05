package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// readAllStdin drains stdin. Kept separate so the prompt path in auth_login has
// one obvious non-interactive counterpart, and so a test can exercise it.
func readAllStdin() (string, error) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// secretPrompt is the wording around one hidden read. The mechanics are the same
// for every secret this tool takes; only what to call it and what to suggest when
// it is absent differ.
type secretPrompt struct {
	// Lead is shown above the prompt on a terminal, and says where the value comes
	// from. It may be empty.
	Lead string
	// Label is the prompt itself, written without a newline.
	Label string
	// Missing is the error when nothing arrives through either channel.
	Missing string
	// NoInput is the error when --no-input rules the prompt out, and names the way
	// a script supplies the value instead.
	NoInput string
}

// readSecret takes a secret from a hidden prompt, or from stdin when there is no
// terminal to prompt on.
//
// A prompt is only ever offered on a TTY. Prompting a non-interactive caller
// blocks on a stdin that never closes, leaving it with no output and no exit
// code — the one failure mode a caller cannot recover from.
func readSecret(cmd *cobra.Command, prompt secretPrompt, fromStdin bool) (string, error) {
	stdin := int(os.Stdin.Fd())
	if fromStdin || noInput || !term.IsTerminal(stdin) {
		if noInput && !fromStdin {
			return "", fmt.Errorf("%s", prompt.NoInput)
		}
		data, err := readAllStdin()
		if err != nil {
			return "", err
		}
		secret := strings.TrimSpace(data)
		if secret == "" {
			return "", fmt.Errorf("%s", prompt.Missing)
		}
		return secret, nil
	}

	if prompt.Lead != "" {
		infof(cmd, "%s", prompt.Lead)
	}
	writef(cmd.ErrOrStderr(), "%s", prompt.Label)
	raw, err := term.ReadPassword(stdin)
	writef(cmd.ErrOrStderr(), "\n")
	if err != nil {
		return "", err
	}
	secret := strings.TrimSpace(string(raw))
	if secret == "" {
		return "", fmt.Errorf("%s", prompt.Missing)
	}
	return secret, nil
}
