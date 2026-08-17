package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestProgressStyleIsOffForAWriterThatIsNotAFile(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.SetErr(&bytes.Buffer{})

	if got := progressStyleFor(cmd); got != progressOff {
		t.Errorf("progressStyleFor() = %v, want progressOff", got)
	}
}

func TestProgressStyleIsLinesWhenStderrIsAPipe(t *testing.T) {
	t.Parallel()

	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() returned error: %v", err)
	}
	defer func() { _ = read.Close() }()
	defer func() { _ = write.Close() }()

	cmd := &cobra.Command{}
	cmd.SetErr(write)

	// The case a bar cannot serve: nothing redraws a pipe, so the alternative to
	// a line stream is silence for the length of the transfer.
	if got := progressStyleFor(cmd); got != progressLines {
		t.Errorf("progressStyleFor() = %v, want progressLines", got)
	}
}

func TestProgressLinesArePacedByTheirOwnInterval(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}
	bar := newProgress(out, "bundle.tar.gz", 1000, progressLines)

	for sent := int64(100); sent <= 500; sent += 100 {
		// Each Set is past the tick throttle, so only the line interval is left to
		// hold anything back.
		bar.lastAt = time.Now().Add(-progressInterval)
		bar.Set(sent)
	}

	if got := strings.Count(out.String(), "\n"); got != 1 {
		t.Errorf("wrote %d line(s) inside one interval, want 1", got)
	}
	if strings.Contains(out.String(), "\r") {
		t.Error("a line stream carries a carriage return, which only a redrawn bar should")
	}
}

func TestProgressDoneReportsTheFinalReadingWhateverTheCadence(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}
	bar := newProgress(out, "bundle.tar.gz", 1000, progressLines)
	bar.lastLineAt = time.Now()
	bar.done = 1000

	bar.Done()

	// A transfer that finishes between two ticks would otherwise stop short of
	// its own total, and the last thing on screen would read as a stall.
	if got := strings.Count(out.String(), "\n"); got != 1 {
		t.Errorf("Done() wrote %d line(s), want 1", got)
	}
	if !strings.Contains(out.String(), "100.0%") {
		t.Errorf("Done() = %q, want it to carry 100.0%%", out.String())
	}
}
