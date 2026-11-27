package main

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

var ansiCodes = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// screen plays what was written to a terminal back as its lines: \r and
// erase-line wipe the line, cursor-up moves back one, clear-screen wipes
// all.
func screen(out string) []string {
	lines := []string{""}
	for len(out) > 0 {
		switch {
		case strings.HasPrefix(out, clearScreen):
			lines, out = []string{""}, out[len(clearScreen):]
		case strings.HasPrefix(out, "\x1b[1A"):
			lines, out = lines[:max(1, len(lines)-1)], out[4:]
		case strings.HasPrefix(out, eraseLine):
			lines[len(lines)-1], out = "", out[len(eraseLine):]
		case strings.HasPrefix(out, "\x1b["):
			loc := ansiCodes.FindStringIndex(out)
			out = out[loc[1]:]
		case out[0] == '\n':
			lines, out = append(lines, ""), out[1:]
		default:
			lines[len(lines)-1] += out[:1]
			out = out[1:]
		}
	}
	return lines
}

func newTestTerm() (*termStatus, *lockedBuffer) {
	var b lockedBuffer
	t := newTermStatus(&b, false)
	t.now = func() time.Time { return time.Date(2026, 1, 2, 12, 4, 1, 0, time.UTC) }
	t.tick = time.Hour
	return t, &b
}

// A rebuild ends as one timed line with its steps and the file it answers.
func TestTermStatusRebuilt(t *testing.T) {
	ts, b := newTestTerm()
	ts.changed([]string{"pages/index.vuka"})
	steps := ts.build()
	steps.step("transpiling 3 packages")
	time.Sleep(60 * time.Millisecond)
	steps.step("compiling")
	if got := screen(b.String()); !strings.HasSuffix(got[len(got)-1], "compiling  0.0s") {
		t.Errorf("pinned line: %q", got[len(got)-1])
	}
	time.Sleep(60 * time.Millisecond)
	steps.done()
	ts.rebuilt(4900 * time.Millisecond)
	got := screen(b.String())
	want := regexp.MustCompile(`^12:04:01 ✓ rebuilt in 4\.9s   pages/index\.vuka — transpile 0\.\ds · compile 0\.\ds$`)
	if len(got) != 2 || !want.MatchString(got[0]) || got[1] != "" {
		t.Errorf("got %q, want %v and no pinned line", got, want)
	}
}

// The same event again counts on its line.
func TestTermStatusRepeat(t *testing.T) {
	ts, b := newTestTerm()
	ts.unchanged()
	ts.unchanged()
	ts.unchanged()
	got := screen(b.String())
	if len(got) != 2 || !strings.HasSuffix(got[0], "(x3)") || !strings.Contains(got[0], "○ unchanged") {
		t.Errorf("got %q", got)
	}
	// Output between them is a new line.
	ts.wrap(b).Write([]byte("GET /\n"))
	ts.unchanged()
	got = screen(b.String())
	if len(got) != 4 || got[1] != "GET /" || strings.Contains(got[2], "(x") {
		t.Errorf("got %q", got)
	}
}

// The app's output goes above the pinned line: it is erased before the
// write and drawn after, and a line left open isn't drawn over.
func TestTermStatusAppOutput(t *testing.T) {
	ts, b := newTestTerm()
	steps := ts.build()
	steps.step("compiling")
	app := ts.wrap(b)
	before := len(b.String())
	app.Write([]byte("listening on :8080\n"))
	out := b.String()[before:]
	i, j, k := strings.Index(out, eraseLine), strings.Index(out, "listening"), strings.LastIndex(out, "compiling")
	if !(i == 0 && i < j && j < k) {
		t.Errorf("erase, write, redraw: %q", out)
	}
	app.Write([]byte("loading"))
	ts.mu.Lock()
	ts.pinLocked(ts.run.line(1))
	ts.mu.Unlock()
	app.Write([]byte(" done\n"))
	got := screen(b.String())
	if got[len(got)-2] != "loading done" || !strings.Contains(got[len(got)-1], "compiling") {
		t.Errorf("got %q", got)
	}
	steps.done()
}

// A failed build stays on the pinned line until one builds, which says
// it fixed them.
func TestTermStatusSticky(t *testing.T) {
	ts, b := newTestTerm()
	ts.build().done()
	ts.failed(buildFailure{shown: "pages/a.vuka:3: trouble\n", errs: 2, at: "pages/a.vuka:3"}, true)
	got := screen(b.String())
	if want := []string{
		"pages/a.vuka:3: trouble",
		"12:04:01 ✗ build failed      pages/a.vuka:3 — the last good build keeps running",
		"✗ 2 errors  pages/a.vuka:3 — last good build serving",
	}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got %q", got)
	}
	steps := ts.build()
	steps.step("compiling")
	if got := screen(b.String()); !strings.Contains(got[len(got)-1], "the last build failed") {
		t.Errorf("building after a failure: %q", got[len(got)-1])
	}
	steps.done()
	if got := screen(b.String()); !strings.HasPrefix(got[len(got)-1], "✗ 2 errors") {
		t.Errorf("still failing: %q", got[len(got)-1])
	}
	ts.clear()
	ts.rebuilt(2100 * time.Millisecond)
	got = screen(b.String())
	if len(got) != 2 || !strings.Contains(got[0], "✓ fixed — rebuilt in 2.1s") {
		t.Errorf("got %q", got)
	}
}

// -no-clear leaves the screen as it is.
func TestTermStatusNoClear(t *testing.T) {
	ts, b := newTestTerm()
	ts.noClear = true
	ts.unchanged()
	ts.failed(buildFailure{shown: "x.go:1: no\n"}, false)
	ts.clear()
	if strings.Contains(b.String(), clearScreen) {
		t.Errorf("cleared: %q", b.String())
	}
	if got := screen(b.String()); !strings.Contains(got[0], "unchanged") {
		t.Errorf("got %q", got)
	}
}

// The markup-only restart has its own verb; an exit is red.
func TestTermStatusUpdatedExited(t *testing.T) {
	ts, b := newTestTerm()
	ts.changed([]string{"pages/index.vuka.html"})
	ts.updated(200 * time.Millisecond)
	ts.exited("exit status 1")
	got := screen(b.String())
	if got[0] != "12:04:01 ✓ updated in 0.2s   pages/index.vuka.html (markup only — same binary)" ||
		got[1] != "12:04:01 ✗ the app exited (exit status 1) waiting for a change" {
		t.Errorf("got %q", got)
	}
}

// The pinned line is cut to the terminal's width.
func TestTermStatusFit(t *testing.T) {
	if got := fit("\x1b[31mabcdef\x1b[0m", 3); ansiCodes.ReplaceAllString(got, "") != "abc" {
		t.Errorf("got %q", got)
	}
}

func TestCutNoClear(t *testing.T) {
	flags, on := cutNoClear([]string{"-race", "-no-clear"})
	if !on || len(flags) != 1 || flags[0] != "-race" {
		t.Errorf("got %v %v", flags, on)
	}
}
