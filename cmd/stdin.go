package cmd

import (
	"io"
	"os"
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
