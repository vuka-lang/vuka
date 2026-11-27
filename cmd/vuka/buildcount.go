package main

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"sync"
	"sync/atomic"
	"time"
)

// pkgCounter counts the packages go build -v names as it compiles them, and
// takes those lines out of the build's output: what goes on to w is what
// the build says without -v. With pass, the lines go on too (the developer
// asked for -v).
type pkgCounter struct {
	w    io.Writer
	pass bool
	n    atomic.Int64
	tick func(n int) // nil: none

	mu  sync.Mutex
	buf []byte
}

// pkgLine is a line that is an import path and nothing else: go build -v's.
// The build's diagnostics hold a colon or a space.
var pkgLine = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.+~\-/]*$`)

func (c *pkgCounter) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf = append(c.buf, p...)
	for {
		i := bytes.IndexByte(c.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		line := c.buf[:i+1]
		c.buf = c.buf[i+1:]
		if pkgLine.Match(bytes.TrimRight(line, "\r\n")) {
			n := int(c.n.Add(1))
			if c.tick != nil {
				c.tick(n)
			}
			if !c.pass {
				continue
			}
		}
		if _, err := c.w.Write(line); err != nil {
			return len(p), err
		}
	}
}

// flush passes on what is left of a line.
func (c *pkgCounter) flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.buf) > 0 {
		c.w.Write(c.buf)
		c.buf = nil
	}
}

// pkgProgress is how far a build of total packages has come: 143/216
// packages; without the total when it is unknown or passed.
func pkgProgress(n, total int) string {
	if total > 0 && n <= total {
		return fmt.Sprintf("%d/%d packages", n, total)
	}
	if n == 1 {
		return "1 package"
	}
	return fmt.Sprintf("%d packages", n)
}

// countPackages has steps show, once the build has run for after, how many
// packages it has compiled of total (0 for unknown). stop ends it.
func countPackages(c *pkgCounter, steps buildSteps, total int, after time.Duration) (stop func()) {
	t0 := time.Now()
	var mu sync.Mutex
	stopped := false
	show := func(n int) {
		mu.Lock()
		defer mu.Unlock()
		if !stopped && n > 0 && time.Since(t0) >= after {
			steps.setDetail(pkgProgress(n, total))
		}
	}
	c.tick = show
	timer := time.AfterFunc(after, func() { show(int(c.n.Load())) })
	return func() {
		timer.Stop()
		mu.Lock()
		stopped = true
		mu.Unlock()
	}
}
