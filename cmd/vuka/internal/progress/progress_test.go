package progress

import (
	"bytes"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// buffer is a bytes.Buffer safe for the task's goroutine and the test.
type buffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func sh(t *testing.T, script string) *exec.Cmd {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	return exec.Command("sh", "-c", script)
}

func task(w *buffer, tty bool) Task {
	return Task{W: w, TTY: tty, Label: "installing npm packages in web/", Done: "npm packages installed in web/",
		Failed: "npm install failed", Still: "still installing", Tick: 20 * time.Millisecond, Heartbeat: 100 * time.Millisecond}
}

func TestNotATerminal(t *testing.T) {
	var w buffer
	if err := task(&w, false).Run(sh(t, "echo noisy; sleep 0.35")); err != nil {
		t.Fatal(err)
	}
	got := w.String()
	if !regexp.MustCompile(`^installing npm packages in web/ …\n(… still installing \(0s\)\n){2,4}✓ npm packages installed in web/ \(0s\)\n$`).MatchString(got) {
		t.Errorf("got %q", got)
	}
	if strings.Contains(got, "noisy") || strings.Contains(got, "\r") || strings.Contains(got, "\x1b") {
		t.Errorf("the tool's output, a spinner or color: %q", got)
	}
}

func TestFailureShowsTail(t *testing.T) {
	var w buffer
	tk := task(&w, false)
	tk.Tail = 3
	err := tk.Run(sh(t, "for i in 1 2 3 4 5; do echo line $i; done; echo oops >&2; exit 3"))
	if err == nil {
		t.Fatal("no error")
	}
	want := "installing npm packages in web/ …\n✗ npm install failed (exit 3)\n  line 4\n  line 5\n  oops\n"
	if got := w.String(); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestStartFailure(t *testing.T) {
	var w buffer
	err := task(&w, false).Run(exec.Command("/no/such/tool"))
	if err == nil || !strings.Contains(w.String(), "✗ npm install failed (fork/exec /no/such/tool") {
		t.Errorf("%v: %q", err, w.String())
	}
}

func TestTerminalSpinnerStops(t *testing.T) {
	var w buffer
	tk := task(&w, true)
	tk.Color = true
	if err := tk.Run(sh(t, "sleep 0.15")); err != nil {
		t.Fatal(err)
	}
	got := w.String()
	if n := strings.Count(got, "\r\x1b[K"); n < 4 {
		t.Errorf("the spinner drew %d frames: %q", n, got)
	}
	if !strings.Contains(got, "⠋ installing npm packages in web/ … 0s") {
		t.Errorf("no spinner line: %q", got)
	}
	end := "\r\x1b[K\x1b[32m✓\x1b[0m npm packages installed in web/ (0s)\n"
	if !strings.HasSuffix(got, end) || strings.Count(got, "\n") != 1 {
		t.Errorf("doesn't end with one finished line: %q", got)
	}
	// nothing is drawn once it has ended
	time.Sleep(60 * time.Millisecond)
	if w.String() != got {
		t.Errorf("drawn after the end: %q", strings.TrimPrefix(w.String(), got))
	}
}

func TestTerminalFailure(t *testing.T) {
	var w buffer
	err := task(&w, true).Run(sh(t, "echo E404 not found; exit 1"))
	if err == nil {
		t.Fatal("no error")
	}
	if got := w.String(); !strings.HasSuffix(got, "\r\x1b[K✗ npm install failed (exit 1)\n  E404 not found\n") {
		t.Errorf("got %q", got)
	}
}

func TestVerboseStreams(t *testing.T) {
	var w, out buffer
	tk := task(&w, true)
	tk.Output = &out
	if err := tk.Run(sh(t, "echo added 12 packages")); err != nil {
		t.Fatal(err)
	}
	if out.String() != "added 12 packages\n" {
		t.Errorf("output %q", out.String())
	}
	if got := w.String(); strings.Contains(got, "\r") || !strings.HasPrefix(got, "installing npm packages in web/ …\n") {
		t.Errorf("a spinner over streamed output: %q", got)
	}
}
