// Package progress shows that a long, quiet step — a package install — is
// still going. On a terminal it is one line updated in place, a spinner and
// the seconds so far; elsewhere (CI, a pipe, vuka run's prefixed log) a line
// when it starts, a heartbeat every ten seconds, and a line when it ends. The
// step's own output is kept, not shown: the tail of it follows a failure.
package progress

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Task is a step to show.
type Task struct {
	W     io.Writer // where the lines go
	TTY   bool      // W is a terminal: a spinner updated in place
	Color bool      // the ✓ and ✗ in color (a terminal without NO_COLOR)

	Label  string // what is going on: "installing npm packages in web/"
	Done   string // how it ended well: "npm packages installed in web/"
	Failed string // how it ended badly: "npm install failed"
	Still  string // the heartbeat's: "still installing"

	// Output, when set, gets the step's output as it comes (a verbose
	// mode); otherwise its tail follows a failure.
	Output io.Writer

	Tick      time.Duration // the spinner's frame (default 100ms)
	Heartbeat time.Duration // the heartbeat off a terminal (default 10s)
	Tail      int           // lines of output shown after a failure (default 30)
}

var frames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// Run runs c as the task's step: its stdout and stderr are the task's, and
// it returns c's error.
func (t Task) Run(c *exec.Cmd) error {
	out := &tail{n: t.Tail}
	if out.n <= 0 {
		out.n = 30
	}
	var w io.Writer = out
	if t.Output != nil {
		w = io.MultiWriter(out, t.Output)
	}
	c.Stdout, c.Stderr = w, w
	s := t.Start()
	err := c.Run()
	s.End(err, out.lines())
	return err
}

// Step is a task under way.
type Step struct {
	t     Task
	start time.Time
	stop  chan struct{}
	done  sync.WaitGroup
	mu    sync.Mutex
}

// Start shows the task has started and keeps showing it is going until End.
func (t Task) Start() *Step {
	if t.Tick <= 0 {
		t.Tick = 100 * time.Millisecond
	}
	if t.Heartbeat <= 0 {
		t.Heartbeat = 10 * time.Second
	}
	if t.Still == "" {
		t.Still = "still going"
	}
	s := &Step{t: t, start: time.Now(), stop: make(chan struct{})}
	if !t.TTY || t.Output != nil {
		fmt.Fprintf(t.W, "%s …\n", t.Label)
	} else {
		s.frame(0)
	}
	s.done.Add(1)
	go s.loop()
	return s
}

func (s *Step) loop() {
	defer s.done.Done()
	every := s.t.Heartbeat
	if s.t.TTY && s.t.Output == nil {
		every = s.t.Tick
	}
	tk := time.NewTicker(every)
	defer tk.Stop()
	for i := 1; ; i++ {
		select {
		case <-s.stop:
			return
		case <-tk.C:
			if s.t.TTY && s.t.Output == nil {
				s.frame(i)
			} else {
				s.mu.Lock()
				fmt.Fprintf(s.t.W, "… %s (%s)\n", s.t.Still, s.since())
				s.mu.Unlock()
			}
		}
	}
}

func (s *Step) frame(i int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(s.t.W, "\r\x1b[K%c %s … %s", frames[i%len(frames)], s.t.Label, s.since())
}

func (s *Step) since() string {
	return fmt.Sprintf("%ds", int(time.Since(s.start).Round(time.Second)/time.Second))
}

// End stops showing the task and writes how it ended: err nil is done;
// otherwise failed, with the exit code when err has one, then output (the
// step's last lines).
func (s *Step) End(err error, output []string) {
	close(s.stop)
	s.done.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.t.TTY && s.t.Output == nil {
		io.WriteString(s.t.W, "\r\x1b[K")
	}
	mark := func(m, color string) string {
		if s.t.Color {
			return "\x1b[" + color + "m" + m + "\x1b[0m"
		}
		return m
	}
	if err == nil {
		fmt.Fprintf(s.t.W, "%s %s (%s)\n", mark("✓", "32"), s.t.Done, s.since())
		return
	}
	why := err.Error()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() >= 0 {
		why = fmt.Sprintf("exit %d", exit.ExitCode())
	}
	fmt.Fprintf(s.t.W, "%s %s (%s)\n", mark("✗", "31"), s.t.Failed, why)
	if s.t.Output == nil {
		for _, l := range output {
			fmt.Fprintln(s.t.W, "  "+l)
		}
	}
}

// tail keeps the last n lines written to it.
type tail struct {
	mu   sync.Mutex
	n    int
	buf  []byte
	keep []string
}

func (t *tail) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, b...)
	for {
		i := bytes.IndexByte(t.buf, '\n')
		if i < 0 {
			break
		}
		t.add(string(t.buf[:i]))
		t.buf = t.buf[i+1:]
	}
	return len(b), nil
}

func (t *tail) add(l string) {
	l = strings.TrimRight(l, " \r")
	if strings.TrimSpace(l) == "" {
		return
	}
	t.keep = append(t.keep, l)
	if len(t.keep) > t.n {
		t.keep = t.keep[len(t.keep)-t.n:]
	}
}

func (t *tail) lines() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.buf) > 0 {
		t.add(string(t.buf))
		t.buf = nil
	}
	return t.keep
}
