package cmd

import (
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/datapointchris/ifiles/config"
)

// progressInterval throttles redraws. A per-write update on a fast local disk
// spends more time rendering than transferring, and terminal scrolling is not
// free.
const progressInterval = 200 * time.Millisecond

// progressLineInterval paces the stream a pipe gets. Each line there is kept
// rather than overwritten, so the redraw rate would be thousands of lines for
// one transfer.
const progressLineInterval = 5 * time.Second

// progressStyle is how a transfer reports itself, which is a property of where
// stderr goes rather than of the transfer.
type progressStyle int

const (
	progressOff progressStyle = iota
	// progressBar rewrites one line in place, which needs a terminal.
	progressBar
	// progressLines appends one line per interval, for a pipe or a log file. A
	// caller reading a pipe gets no redraws, so silence for the length of a
	// multi-minute transfer is indistinguishable from a hang.
	progressLines
)

// progressStyleFor picks the style from where stderr actually goes. A writer
// that is not a file at all is a caller collecting output in memory, which
// wants neither form.
func progressStyleFor(cmd *cobra.Command) progressStyle {
	file, ok := cmd.ErrOrStderr().(*os.File)
	if !ok {
		return progressOff
	}
	if term.IsTerminal(int(file.Fd())) {
		return progressBar
	}
	return progressLines
}

// progress renders transfer progress to stderr. It goes to stderr because
// stdout is reserved for data — `ifiles read` piped into another program must
// not carry a progress bar.
type progress struct {
	out        io.Writer
	label      string
	total      int64
	done       int64
	started    time.Time
	lastAt     time.Time
	lastLineAt time.Time
	style      progressStyle
	// onTick fires on the same throttle as a redraw, for work that should happen
	// periodically during a transfer but not on every read — persisting a resume
	// offset is a file write, and one per Read would dominate the transfer.
	onTick func(done int64)
}

func newProgress(out io.Writer, label string, total int64, style progressStyle) *progress {
	now := time.Now()
	// lastLineAt is left at its zero value so a pipe gets its first line on the
	// first tick. A transfer that has started and one that is stuck opening the
	// connection look the same until something is written.
	return &progress{
		out:     out,
		label:   label,
		total:   total,
		started: now,
		lastAt:  now,
		style:   style,
	}
}

// Add advances the counter by n bytes.
func (p *progress) Add(n int64) { p.Set(p.done + n) }

// Set reports an absolute byte count, which is what a chunked upload knows.
//
// The throttle gates onTick as well as the redraw, and gates it even when nothing
// is being displayed: resume state has to be kept whether or not stderr is a
// terminal.
func (p *progress) Set(done int64) {
	p.done = done
	if time.Since(p.lastAt) < progressInterval {
		return
	}
	p.lastAt = time.Now()
	if p.onTick != nil {
		p.onTick(done)
	}
	p.draw()
}

// draw emits at whatever cadence the style asks for. The bar redraws on every
// tick; the line stream is paced far slower by its own clock.
func (p *progress) draw() {
	switch p.style {
	case progressBar:
		p.render("\r", "\x1b[K")
	case progressLines:
		if time.Since(p.lastLineAt) < progressLineInterval {
			return
		}
		p.lastLineAt = time.Now()
		p.render("", "\n")
	}
}

// Done draws the final state and moves off the progress line. It ignores the
// line cadence: a transfer finishing between two ticks would otherwise stop
// short of its own total.
func (p *progress) Done() {
	switch p.style {
	case progressBar:
		p.render("\r", "\x1b[K")
		writef(p.out, "\n")
	case progressLines:
		p.render("", "\n")
	}
}

// render writes one reading, wrapped in whatever the style needs around it: \r
// plus a clear-to-end-of-line for a bar, so a shorter reading never leaves
// characters from the previous one behind, and a plain newline for a stream.
func (p *progress) render(prefix, suffix string) {
	elapsed := time.Since(p.started).Seconds()
	rate := ""
	if elapsed > 0 {
		rate = "  " + config.FormatSize(int64(float64(p.done)/elapsed)) + "/s"
	}

	if p.total > 0 {
		percent := float64(p.done) / float64(p.total) * 100
		writef(p.out, "%s%s  %s / %s  %5.1f%%%s%s",
			prefix, p.label, config.FormatSize(p.done), config.FormatSize(p.total), percent, rate, suffix)
		return
	}
	writef(p.out, "%s%s  %s%s%s", prefix, p.label, config.FormatSize(p.done), rate, suffix)
}

// countingReader advances a progress as it is read, for a download where the
// byte count comes from the copy rather than from chunk accounting.
type countingReader struct {
	reader   io.Reader
	progress *progress
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.reader.Read(p)
	if n > 0 {
		c.progress.Add(int64(n))
	}
	return n, err
}
