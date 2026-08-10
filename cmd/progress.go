package cmd

import (
	"io"
	"time"

	"github.com/datapointchris/ifiles/v3/config"
)

// progressInterval throttles redraws. A per-write update on a fast local disk
// spends more time rendering than transferring, and terminal scrolling is not
// free.
const progressInterval = 200 * time.Millisecond

// progress renders transfer progress to stderr, clipped to one line that is
// rewritten in place. It goes to stderr because stdout is reserved for data —
// `ifiles read` piped into another program must not carry a progress bar.
type progress struct {
	out     io.Writer
	label   string
	total   int64
	done    int64
	started time.Time
	lastAt  time.Time
	enabled bool
	// onTick fires on the same throttle as a redraw, for work that should happen
	// periodically during a transfer but not on every read — persisting a resume
	// offset is a file write, and one per Read would dominate the transfer.
	onTick func(done int64)
}

func newProgress(out io.Writer, label string, total int64, enabled bool) *progress {
	now := time.Now()
	return &progress{
		out:     out,
		label:   label,
		total:   total,
		started: now,
		lastAt:  now,
		enabled: enabled,
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
	if p.enabled {
		p.render()
	}
}

// Done draws the final state and moves off the progress line.
func (p *progress) Done() {
	if !p.enabled {
		return
	}
	p.render()
	writef(p.out, "\n")
}

func (p *progress) render() {
	elapsed := time.Since(p.started).Seconds()
	rate := ""
	if elapsed > 0 {
		rate = "  " + config.FormatSize(int64(float64(p.done)/elapsed)) + "/s"
	}

	// \r plus a trailing clear-to-end-of-line, so a shorter line never leaves
	// characters from the previous one behind.
	if p.total > 0 {
		percent := float64(p.done) / float64(p.total) * 100
		writef(p.out, "\r%s  %s / %s  %5.1f%%%s\x1b[K",
			p.label, config.FormatSize(p.done), config.FormatSize(p.total), percent, rate)
		return
	}
	writef(p.out, "\r%s  %s%s\x1b[K", p.label, config.FormatSize(p.done), rate)
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
