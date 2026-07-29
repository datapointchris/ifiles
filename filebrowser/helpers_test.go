package filebrowser

import (
	"errors"
	"strings"
)

// asError wraps errors.As so the assertions above read as one line. Generics keep
// the target concrete, which errors.As requires anyway.
func asError[T error](err error, target *T) bool { return errors.As(err, target) }

func containsText(haystack, needle string) bool { return strings.Contains(haystack, needle) }
