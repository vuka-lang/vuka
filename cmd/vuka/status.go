package main

import (
	"fmt"
	"io"
	"time"
)

// watchStatus is what vuka run -watch tells the developer: the steps of a
// build as they run, and what each build came to.
type watchStatus interface {
	build() buildSteps // the steps of one build
	linked(msg string) // what auto-link added, once per change
	reused()           // a kept build of an earlier run started
	failed(shown string, running bool)
	warn(msg string)
	unchanged()
	rebuilt(took time.Duration)
	exited(status string) // the app exited by itself
	clear()               // the errors on the screen are gone
}

// buildSteps hears a build's steps: step as one begins, done once they are
// over.
type buildSteps interface {
	step(name string)
	done()
}

// plainStatus says it in ● lines, the way a pipe or a log takes it. On a
// terminal a failed build's errors replace the last one's on the screen.
type plainStatus struct {
	w   io.Writer
	tty bool
	now func() time.Time // nil: time.Now
}

func (p *plainStatus) build() buildSteps { return newPhases(p.w) }

func (p *plainStatus) linked(msg string) { fmt.Fprintln(p.w, "● "+msg) }

func (p *plainStatus) reused() { fmt.Fprintln(p.w, "● reusing the last build (no changes)") }

func (p *plainStatus) failed(shown string, running bool) {
	at := ""
	if p.tty {
		io.WriteString(p.w, clearScreen)
		now := p.now
		if now == nil {
			now = time.Now
		}
		at = " at " + now().Format("15:04:05")
	}
	io.WriteString(p.w, shown)
	if running {
		fmt.Fprintf(p.w, "● build failed%s; the last good build keeps running\n", at)
	} else {
		fmt.Fprintf(p.w, "● build failed%s; waiting for a change\n", at)
	}
}

func (p *plainStatus) warn(msg string) { fmt.Fprintln(p.w, "● warning: "+msg) }

func (p *plainStatus) unchanged() { fmt.Fprintln(p.w, "● unchanged") }

func (p *plainStatus) rebuilt(took time.Duration) {
	fmt.Fprintf(p.w, "● rebuilt in %.1fs\n", took.Seconds())
}

func (p *plainStatus) exited(status string) {
	fmt.Fprintf(p.w, "● the app exited (%s); waiting for a change\n", status)
}

func (p *plainStatus) clear() {
	if p.tty {
		io.WriteString(p.w, clearScreen)
	}
}

// status is the supervisor's watchStatus: plain over stderr unless one is
// set.
func (s *supervisor) status() watchStatus {
	if s.st != nil {
		return s.st
	}
	return &plainStatus{w: s.stderr, tty: s.tty}
}
