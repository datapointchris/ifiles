package cmd

import "testing"

func TestResolveVersionPrefersLdflags(t *testing.T) {
	t.Parallel()

	if got := resolveVersion("1.2.3", "v9.9.9"); got != "1.2.3" {
		t.Errorf("resolveVersion() = %q, want the ldflags value 1.2.3", got)
	}
}

func TestResolveVersionRejectsAPseudoVersion(t *testing.T) {
	t.Parallel()

	// Go stamps this onto a local `go build`, and it is valid semver, so treating
	// it as a release would let a dev build try to self-update.
	pseudo := "v1.6.1-0.20260724161156-2c04703"
	if got := resolveVersion("dev", pseudo); got != "dev" {
		t.Errorf("resolveVersion() = %q, want dev for a pseudo-version", got)
	}
}

func TestResolveVersionAcceptsARealModuleVersion(t *testing.T) {
	t.Parallel()

	// This is the `go install pkg@latest` path, which applies no ldflags but does
	// stamp a real module version into build info.
	if got := resolveVersion("dev", "v0.3.1"); got != "0.3.1" {
		t.Errorf("resolveVersion() = %q, want 0.3.1", got)
	}
}
