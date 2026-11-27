package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// go build -v's package lines are counted and taken out; anything else goes
// on as it came, in pieces or not.
func TestPkgCounter(t *testing.T) {
	var out bytes.Buffer
	var ticks []int
	c := &pkgCounter{w: &out, tick: func(n int) { ticks = append(ticks, n) }}
	in := "internal/goarch\nexample.com/shop/orders\n# example.com/shop/orders\norders/orders.go:3:2: undefined: x\nunicode/utf8\r\ngo: downloading example.com/dep v1.2.0\ncommand-line-arguments\nno newline"
	for _, piece := range []string{in[:10], in[10:37], in[37:]} {
		c.Write([]byte(piece))
	}
	c.flush()
	want := "# example.com/shop/orders\norders/orders.go:3:2: undefined: x\ngo: downloading example.com/dep v1.2.0\nno newline"
	if out.String() != want {
		t.Errorf("passed on %q, want %q", out.String(), want)
	}
	if got := c.n.Load(); got != 4 || len(ticks) != 4 || ticks[3] != 4 {
		t.Errorf("counted %d, ticks %v; want 4", got, ticks)
	}
	// The developer's own -v keeps the lines.
	out.Reset()
	c = &pkgCounter{w: &out, pass: true}
	c.Write([]byte("example.com/shop/orders\nx.go:1:1: bad\n"))
	if out.String() != "example.com/shop/orders\nx.go:1:1: bad\n" || c.n.Load() != 1 {
		t.Errorf("with pass: %q, %d", out.String(), c.n.Load())
	}
}

func TestPkgProgress(t *testing.T) {
	for _, c := range []struct {
		n, total int
		want     string
	}{
		{143, 216, "143/216 packages"},
		{143, 0, "143 packages"},
		{220, 216, "220 packages"},
		{1, 0, "1 package"},
	} {
		if got := pkgProgress(c.n, c.total); got != c.want {
			t.Errorf("pkgProgress(%d, %d) = %q, want %q", c.n, c.total, got, c.want)
		}
	}
}

// The count shows once the build has run for a while, not before, and not
// after it ended.
func TestCountPackages(t *testing.T) {
	steps := &detailSteps{}
	c := &pkgCounter{w: &bytes.Buffer{}}
	stop := countPackages(c, steps, 216, 50*time.Millisecond)
	c.Write([]byte("a\nb\n"))
	if d := steps.get(); d != "" {
		t.Errorf("shown before the wait: %q", d)
	}
	time.Sleep(100 * time.Millisecond)
	if d := steps.get(); d != "2/216 packages" {
		t.Errorf("after the wait: %q", d)
	}
	c.Write([]byte("c\n"))
	if d := steps.get(); d != "3/216 packages" {
		t.Errorf("after a package: %q", d)
	}
	stop()
	c.Write([]byte("d\n"))
	if d := steps.get(); d != "3/216 packages" {
		t.Errorf("after the build: %q", d)
	}
}

type detailSteps struct {
	mu     sync.Mutex
	detail string
}

func (s *detailSteps) step(string) {}
func (s *detailSteps) done()       {}
func (s *detailSteps) setDetail(d string) {
	s.mu.Lock()
	s.detail = d
	s.mu.Unlock()
}
func (s *detailSteps) get() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.detail
}

// A failing build run with -v, its output through the counter, says what
// it says without -v.
func TestPkgCounterFailingBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go build")
	}
	dir := t.TempDir()
	stamp := time.Now().Format("150405.000000000") // a package the build cache hasn't seen
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/shop\n\ngo 1.21\n")
	writeFile(t, filepath.Join(dir, "price", "price.go"), "package price\n\nconst Stamp = \""+stamp+"\"\n\nfunc Of(n int) int { return n * 2 }\n")
	writeFile(t, filepath.Join(dir, "main.go"), "package main\n\nimport \"example.com/shop/price\"\n\nfunc main() {\n\tvar s string = price.Of(1)\n\t_ = undefinedName\n\t_ = s\n}\n")
	build := func(args ...string) ([]byte, error) {
		c := exec.Command("go", append(append([]string{"build"}, args...), "-o", filepath.Join(dir, "app"), ".")...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
		var out bytes.Buffer
		cnt := &pkgCounter{w: &out}
		c.Stdout, c.Stderr = cnt, cnt
		err := c.Run()
		cnt.flush()
		if len(args) > 0 && cnt.n.Load() == 0 {
			t.Errorf("-v named no package")
		}
		return out.Bytes(), err
	}
	verbose, err := build("-v")
	if err == nil {
		t.Fatal("the build passed")
	}
	plain, err := build()
	if err == nil {
		t.Fatal("the build passed")
	}
	if !bytes.Equal(verbose, plain) {
		t.Errorf("with -v:\n%s\nwithout:\n%s", verbose, plain)
	}
	if !strings.Contains(string(plain), "undefinedName") {
		t.Errorf("no diagnostics: %s", plain)
	}
}
