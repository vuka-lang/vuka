package main

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// phases says what a rebuild under vuka run -watch is doing once a step of
// it has taken longer than slowPhase: a first build on a cold cache, or a
// slow disk, otherwise looks like a hang.
type phases struct {
	w     io.Writer
	after time.Duration

	mu    sync.Mutex
	name  string
	start time.Time
	timer *time.Timer
	said  bool // the step was named as slow
	n     int  // the step's number
}

// slowPhase is how long a step runs before it is named.
var slowPhase = 3 * time.Second

func newPhases(w io.Writer) *phases { return &phases{w: w, after: slowPhase} }

// step ends the step before and starts name.
func (p *phases) step(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopLocked()
	p.n++
	n := p.n
	p.name, p.start = name, time.Now()
	p.timer = time.AfterFunc(p.after, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.n == n && p.name != "" && !p.said {
			p.said = true
			fmt.Fprintf(p.w, "● %s… (%.0fs so far)\n", name, time.Since(p.start).Seconds())
		}
	})
}

// done ends the last step.
func (p *phases) done() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopLocked()
}

func (p *phases) stopLocked() {
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	if p.said {
		fmt.Fprintf(p.w, "● %s took %.1fs\n", p.name, time.Since(p.start).Seconds())
	}
	p.name, p.said = "", false
}
