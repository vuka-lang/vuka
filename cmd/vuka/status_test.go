package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// The plain status says each event as the watch always has.
func TestPlainStatusLines(t *testing.T) {
	at := func() time.Time { return time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC) }
	for _, c := range []struct {
		name string
		tty  bool
		say  func(watchStatus)
		want string
	}{
		{"linked", false, func(s watchStatus) { s.linked("linking 1 declarer package (pages/alpha)") }, "● linking 1 declarer package (pages/alpha)\n"},
		{"reused", false, watchStatus.reused, "● reusing the last build (no changes)\n"},
		{"failed running", false, func(s watchStatus) { s.failed("main.vuka:3: trouble\n", true) },
			"main.vuka:3: trouble\n● build failed; the last good build keeps running\n"},
		{"failed waiting", false, func(s watchStatus) { s.failed("main.vuka:3: trouble\n", false) },
			"main.vuka:3: trouble\n● build failed; waiting for a change\n"},
		{"failed on a terminal", true, func(s watchStatus) { s.failed("main.vuka:3: trouble\n", true) },
			clearScreen + "main.vuka:3: trouble\n● build failed at 15:04:05; the last good build keeps running\n"},
		{"warn", false, func(s watchStatus) { s.warn("something is off") }, "● warning: something is off\n"},
		{"unchanged", false, watchStatus.unchanged, "● unchanged\n"},
		{"rebuilt", false, func(s watchStatus) { s.rebuilt(4900 * time.Millisecond) }, "● rebuilt in 4.9s\n"},
		{"exited", false, func(s watchStatus) { s.exited("exit status 1") }, "● the app exited (exit status 1); waiting for a change\n"},
		{"clear", false, watchStatus.clear, ""},
		{"clear on a terminal", true, watchStatus.clear, clearScreen},
	} {
		var b bytes.Buffer
		c.say(&plainStatus{w: &b, tty: c.tty, now: at})
		if got := b.String(); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// A build's steps are the phases, over the status's writer.
func TestPlainStatusSteps(t *testing.T) {
	defer func(d time.Duration) { slowPhase = d }(slowPhase)
	slowPhase = time.Millisecond
	var b lockedBuffer
	steps := (&plainStatus{w: &b}).build()
	steps.step("compiling")
	time.Sleep(50 * time.Millisecond)
	steps.done()
	if got := b.String(); !strings.HasPrefix(got, "● compiling… (0s so far)\n● compiling took 0.") {
		t.Errorf("got %q", got)
	}
}
