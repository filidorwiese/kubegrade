// Package progress renders a single-line progress bar on stderr. It is a
// no-op when stderr is not a terminal, so piped or JSON output stays clean.
package progress

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// Func reports progress of a phase. total may be 0 when unknown.
type Func func(phase string, done, total int)

func Nop(string, int, int) {}

type Bar struct {
	mu sync.Mutex
	w  io.Writer
}

// New returns a bar writing to stderr, or a no-op when stderr isn't a tty.
func New() (*Bar, Func) {
	fi, err := os.Stderr.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return &Bar{}, Nop
	}
	b := &Bar{w: os.Stderr}
	return b, b.Update
}

func (b *Bar) Update(phase string, done, total int) {
	if b.w == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	const width = 20
	var line string
	if total > 0 {
		filled := width * done / total
		line = fmt.Sprintf("[%s%s] %d/%d %s", strings.Repeat("#", filled), strings.Repeat(" ", width-filled), done, total, phase)
	} else {
		line = fmt.Sprintf("[%s] %s", strings.Repeat(" ", width), phase)
	}
	fmt.Fprintf(b.w, "\r\033[K%s", line)
}

// Done clears the bar so the report starts on a clean line.
func (b *Bar) Done() {
	if b.w == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	fmt.Fprint(b.w, "\r\033[K")
}
