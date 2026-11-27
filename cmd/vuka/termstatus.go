package main

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// termStatus is the watch's status on a terminal: the build under way on
// one line pinned at the bottom, redrawn in place, and each event a line of
// its own above it, timed. The app's output goes through wrap, which takes
// the pinned line away while it writes.
type termStatus struct {
	w       io.Writer // the terminal
	noClear bool      // a failed build's errors don't clear the screen
	now     func() time.Time
	width   func() int // the terminal's columns; 0 for unknown
	tick    time.Duration

	mu      sync.Mutex
	pin     string // the pinned line, "" for none
	drawn   bool   // it is on the screen
	midLine bool   // what was written last didn't end its line
	last    string // the bottom event line without its time; "" when output followed it
	repeat  int    // how many times in a row it was said

	run     *termSteps // the build under way
	took    []stepTime // the steps of the last build that ended
	files   []string   // the changes the next build answers
	failing string     // the last build's failure as pinned; "" when it built
	fixed   bool       // the errors went: the next build says so
	frame   int
	stop    chan struct{}
}

type stepTime struct {
	name string
	took time.Duration
}

const (
	ansiReset = "\x1b[0m"
	ansiDim   = "\x1b[2m"
	ansiRed   = "\x1b[31m"
	ansiGreen = "\x1b[32m"
	ansiAmber = "\x1b[33m"
	ansiCyan  = "\x1b[36m"
	eraseLine = "\r\x1b[K"
)

func newTermStatus(w io.Writer, noClear bool) *termStatus {
	t := &termStatus{w: w, noClear: noClear, tick: 100 * time.Millisecond}
	if f, ok := w.(*os.File); ok {
		t.width = func() int { return termWidth(f) }
	}
	return t
}

// wrap is w with the pinned line taken away around each write.
func (t *termStatus) wrap(w io.Writer) io.Writer { return &termOut{t: t, w: w} }

// termOut is a writer of the app's (or anything else's) on the status's
// terminal.
type termOut struct {
	t *termStatus
	w io.Writer
}

func (o *termOut) Write(p []byte) (int, error) {
	o.t.mu.Lock()
	defer o.t.mu.Unlock()
	o.t.eraseLocked()
	n, err := o.w.Write(p)
	if n > 0 {
		o.t.midLine = p[n-1] != '\n'
		o.t.last = ""
	}
	o.t.drawLocked()
	return n, err
}

// termFile is the file w writes to, seen through the status's wrapper.
func termFile(w io.Writer) (*os.File, bool) {
	if o, ok := w.(*termOut); ok {
		w = o.w
	}
	f, ok := w.(*os.File)
	return f, ok
}

func (t *termStatus) at() string {
	now := time.Now
	if t.now != nil {
		now = t.now
	}
	return now().Format("15:04:05")
}

func (t *termStatus) eraseLocked() {
	if t.drawn {
		io.WriteString(t.w, eraseLine)
		t.drawn = false
	}
}

// drawLocked puts the pinned line back, unless a line was left open.
func (t *termStatus) drawLocked() {
	if t.pin == "" || t.midLine || t.drawn {
		return
	}
	line := t.pin
	if t.width != nil {
		if n := t.width(); n > 1 {
			line = fit(line, n-1) // a wrapped line isn't erased by \r
		}
	}
	io.WriteString(t.w, line)
	t.drawn = true
}

func (t *termStatus) pinLocked(line string) {
	t.eraseLocked()
	t.pin = line
	t.drawLocked()
}

// say persists an event: the time, the symbol in its colour, the verb, the
// detail dim. Said again in a row it is the same line, counted.
func (t *termStatus) say(color, sym, verb, detail string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sayLocked(color, sym, verb, detail)
}

func (t *termStatus) sayLocked(color, sym, verb, detail string) {
	key := sym + " " + verb + " " + detail
	line := color + sym + ansiReset + " " + verb
	if detail != "" {
		line += strings.Repeat(" ", max(1, 18-utf8.RuneCountInString(verb))) + ansiDim + detail + ansiReset
	}
	t.eraseLocked()
	if t.midLine {
		io.WriteString(t.w, "\n")
		t.midLine = false
	}
	if key == t.last {
		t.repeat++
		io.WriteString(t.w, "\x1b[1A"+eraseLine)
		line += fmt.Sprintf(" %s(x%d)%s", ansiDim, t.repeat, ansiReset)
	} else {
		t.last, t.repeat = key, 1
	}
	io.WriteString(t.w, ansiDim+t.at()+ansiReset+" "+line+"\n")
	t.drawLocked()
}

func (t *termStatus) clearScreenLocked() {
	io.WriteString(t.w, clearScreen)
	t.drawn, t.midLine, t.last = false, false, ""
}

func (t *termStatus) build() buildSteps {
	s := &termSteps{t: t}
	t.mu.Lock()
	t.run = s
	if t.stop == nil {
		t.stop = make(chan struct{})
		go t.spin(t.stop)
	}
	t.mu.Unlock()
	return s
}

func (t *termStatus) spin(stop chan struct{}) {
	tk := time.NewTicker(t.tick)
	defer tk.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tk.C:
			t.mu.Lock()
			if t.run != nil && t.run.name != "" {
				t.frame++
				t.pinLocked(t.run.line(t.frame))
			}
			t.mu.Unlock()
		}
	}
}

// termSteps is a build's steps on the pinned line.
type termSteps struct {
	t      *termStatus
	name   string
	start  time.Time
	detail string
	took   []stepTime
}

func (s *termSteps) step(name string) {
	s.t.mu.Lock()
	defer s.t.mu.Unlock()
	s.endLocked()
	s.name, s.start, s.detail = name, time.Now(), ""
	if s.t.run == s {
		s.t.pinLocked(s.line(s.t.frame))
	}
}

// setDetail says more about the step under way: how far it has come.
func (s *termSteps) setDetail(d string) {
	s.t.mu.Lock()
	defer s.t.mu.Unlock()
	s.detail = d
}

func (s *termSteps) done() {
	t := s.t
	t.mu.Lock()
	defer t.mu.Unlock()
	s.endLocked()
	if t.run != s {
		return
	}
	t.run, t.took = nil, s.took
	if t.stop != nil {
		close(t.stop)
		t.stop = nil
	}
	t.pinLocked(t.failing)
}

func (s *termSteps) endLocked() {
	if s.name != "" {
		s.took = append(s.took, stepTime{s.name, time.Since(s.start)})
		s.name = ""
	}
}

// line is the pinned line of the step under way: ⠹ compiling  3.1s.
func (s *termSteps) line(frame int) string {
	color := ansiCyan
	if s.t.failing != "" {
		color = ansiRed
	}
	l := fmt.Sprintf("%s%c%s %s  %.1fs", color, spinFrames[frame%len(spinFrames)], ansiReset, s.name, time.Since(s.start).Seconds())
	if s.detail != "" {
		l += "   " + ansiDim + s.detail + ansiReset
	}
	if s.t.failing != "" {
		l += ansiDim + "  · the last build failed" + ansiReset
	}
	return l
}

var spinFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// stepWords name a step in the breakdown of a rebuild.
var stepWords = map[string]string{"scanning": "scan", "transpiling": "transpile", "linking": "link", "compiling": "compile"}

// breakdown is how long the last build's steps took: transpile 0.8s ·
// compile 3.2s; steps under 50ms are left out.
func (t *termStatus) breakdown() string {
	var parts []string
	for _, st := range t.took {
		if st.took < 50*time.Millisecond {
			continue
		}
		word, _, _ := strings.Cut(st.name, " ")
		if w, ok := stepWords[word]; ok {
			word = w
		}
		parts = append(parts, fmt.Sprintf("%s %.1fs", word, st.took.Seconds()))
	}
	return strings.Join(parts, " · ")
}

// changedFiles names the files the build answers, and takes them.
func (t *termStatus) changedFiles() string {
	f := t.files
	t.files = nil
	switch len(f) {
	case 0:
		return ""
	case 1:
		return f[0]
	}
	return fmt.Sprintf("%s +%d", f[0], len(f)-1)
}

func (t *termStatus) changed(files []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, f := range files {
		if !slices.Contains(t.files, f) {
			t.files = append(t.files, f)
		}
	}
}

func (t *termStatus) linked(msg string) { t.say(ansiDim, "↻", ansiDim+msg+ansiReset, "") }

func (t *termStatus) reused() { t.say(ansiDim, "○", "reusing the last build", "no changes") }

func (t *termStatus) warn(msg string) { t.say(ansiAmber, "!", "warning: "+msg, "") }

func (t *termStatus) failed(f buildFailure, running bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.eraseLocked()
	if !t.noClear {
		t.clearScreenLocked()
	} else if t.midLine {
		io.WriteString(t.w, "\n")
		t.midLine = false
	}
	io.WriteString(t.w, f.shown)
	t.last, t.files, t.fixed = "", nil, false
	then, serving := "waiting for a change", "waiting for a change"
	if running {
		then, serving = "the last good build keeps running", "last good build serving"
	}
	where := then
	if f.at != "" {
		where = f.at + " — " + then
	}
	t.sayLocked(ansiRed, "✗", "build failed", where)
	t.last = ""
	pin := ansiRed + "✗ build failed" + ansiReset
	if f.errs > 0 {
		noun := "errors"
		if f.errs == 1 {
			noun = "error"
		}
		pin = fmt.Sprintf("%s✗ %d %s%s", ansiRed, f.errs, noun, ansiReset)
	}
	if f.at != "" {
		pin += "  " + f.at
	}
	t.failing = pin + ansiDim + " — " + serving + ansiReset
	t.pinLocked(t.failing)
}

func (t *termStatus) clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.failing == "" {
		return
	}
	t.eraseLocked()
	if !t.noClear {
		t.clearScreenLocked()
	}
	t.failing, t.fixed = "", true
	t.pinLocked("")
}

func (t *termStatus) unchanged() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.files = nil
	if t.fixed {
		t.fixed = false
		t.sayLocked(ansiGreen, "✓", "fixed — unchanged", "the binary is the one running")
		return
	}
	t.sayLocked(ansiDim, "○", ansiDim+"unchanged"+ansiReset, "the edit didn't change the binary")
}

func (t *termStatus) rebuilt(took time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	verb := fmt.Sprintf("rebuilt in %.1fs", took.Seconds())
	if t.fixed {
		verb, t.fixed = "fixed — "+verb, false
	}
	detail := t.changedFiles()
	if b := t.breakdown(); b != "" {
		if detail != "" {
			detail += " — "
		}
		detail += b
	}
	t.sayLocked(ansiGreen, "✓", verb, detail)
}

func (t *termStatus) updated(took time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	verb := fmt.Sprintf("updated in %.1fs", took.Seconds())
	if t.fixed {
		verb, t.fixed = "fixed — "+verb, false
	}
	detail := "(markup only — same binary)"
	if f := t.changedFiles(); f != "" {
		detail = f + " " + detail
	}
	t.sayLocked(ansiGreen, "✓", verb, detail)
}

func (t *termStatus) exited(status string) {
	t.say(ansiRed, "✗", "the app exited ("+status+")", "waiting for a change")
}

// fit cuts s, which may hold colour codes, to n columns.
func fit(s string, n int) string {
	cols := 0
	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			j := strings.IndexByte(s[i:], 'm')
			if j < 0 {
				break
			}
			i += j + 1
			continue
		}
		if cols == n {
			return s[:i] + ansiReset
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		cols++
	}
	return s
}
