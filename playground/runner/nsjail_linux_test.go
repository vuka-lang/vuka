package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The nsjail tests run in the runner's image (RUNNER_NSJAIL_TEST=1), as root
// in a container started with --privileged --cgroupns=private.

func jail(t *testing.T, edit func(*config)) (*nsjail, *template) {
	t.Helper()
	if os.Getenv("RUNNER_NSJAIL_TEST") != "1" {
		t.Skip("RUNNER_NSJAIL_TEST=1 runs it, in the runner's image")
	}
	cfg := loadConfig()
	cfg.vuka = "/usr/local/bin/vuka"
	if edit != nil {
		edit(&cfg)
	}
	sb, err := newNsjail(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := loadTemplate(cfg.gomod, cfg.vukaSrc)
	if err != nil {
		t.Fatal(err)
	}
	return sb.(*nsjail), tmpl
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// runMain builds a main package of src in the jail and runs it until it
// exits or d passes; its output and how it ended.
func runMain(t *testing.T, j *nsjail, tmpl *template, src string, d time.Duration) (string, string, time.Duration) {
	t.Helper()
	dir := t.TempDir()
	os.Chmod(dir, 0o755)
	srcDir, data := filepath.Join(dir, "src"), filepath.Join(dir, "data")
	os.MkdirAll(srcDir, 0o755)
	os.MkdirAll(data, 0o755)
	if err := tmpl.write(srcDir, map[string]string{"main.go": src}); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	if err := j.Build(context.Background(), srcDir, &log); err != nil {
		t.Fatalf("build: %v\n%s", err, log.String())
	}
	out := &syncBuf{}
	start := time.Now()
	p, err := j.Start(context.Background(), filepath.Join(srcDir, "app"), data, nil, out)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.Done:
	case <-time.After(d):
		p.Stop()
		t.Fatalf("still running after %s:\n%s", d, out.String())
	}
	return out.String(), p.Exit(), time.Since(start)
}

func TestNsjailNoEgress(t *testing.T) {
	j, tmpl := jail(t, nil)
	out, _, _ := runMain(t, j, tmpl, `package main

import (
	"fmt"
	"net"
	"net/http"
	"time"
)

func main() {
	for _, a := range []string{"1.1.1.1:80", "8.8.8.8:53", "[2606:4700:4700::1111]:80"} {
		c, err := net.DialTimeout("tcp", a, 3*time.Second)
		if err == nil {
			c.Close()
			fmt.Println("CONNECTED", a)
		} else {
			fmt.Println("refused", a, err)
		}
	}
	if _, err := http.Get("http://example.com/"); err == nil {
		fmt.Println("CONNECTED example.com")
	}
}
`, 30*time.Second)
	if strings.Contains(out, "CONNECTED") || !strings.Contains(out, "refused 1.1.1.1:80") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestNsjailFilesystem(t *testing.T) {
	j, tmpl := jail(t, nil)
	out, _, _ := runMain(t, j, tmpl, `package main

import (
	"fmt"
	"os"
)

func main() {
	for _, p := range []string{"/etc/passwd", "/usr/local/go/VERSION", "/opt/gocache/README", "/var/lib/runner", "/proc/1/root/etc/hostname", "../src/main.go"} {
		if _, err := os.ReadFile(p); err == nil {
			fmt.Println("READ", p)
		}
	}
	if err := os.WriteFile("/app", []byte("x"), 0o644); err == nil {
		fmt.Println("WROTE /app")
	}
	if err := os.WriteFile("/data/app.txt", []byte("x"), 0o644); err != nil {
		fmt.Println("no data dir:", err)
	}
	ents, _ := os.ReadDir("/")
	for _, e := range ents {
		fmt.Println("root:", e.Name())
	}
}
`, 30*time.Second)
	if strings.Contains(out, "READ") || strings.Contains(out, "WROTE") || strings.Contains(out, "no data dir") {
		t.Fatalf("output:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if name, ok := strings.CutPrefix(line, "root: "); ok && !strings.Contains(" app data dev proc shim sock tmp ", " "+name+" ") {
			t.Errorf("the jail's root holds %s", name)
		}
	}
}

func TestNsjailForkBomb(t *testing.T) {
	j, tmpl := jail(t, nil)
	out, _, _ := runMain(t, j, tmpl, `package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

func main() {
	if os.Getenv("CHILD") == "1" {
		time.Sleep(time.Minute)
		return
	}
	n := 0
	for i := 0; i < 1000; i++ {
		c := exec.Command("/app")
		c.Env = []string{"CHILD=1"}
		if err := c.Start(); err != nil {
			fmt.Println("refused:", err)
			break
		}
		n++
	}
	fmt.Println("spawned", n)
	os.Exit(0)
}
`, 60*time.Second)
	if strings.Contains(out, "spawned 1000") {
		t.Fatalf("the fork bomb spawned freely:\n%s", out)
	}
}

func TestNsjailMemory(t *testing.T) {
	j, tmpl := jail(t, nil)
	out, exit, _ := runMain(t, j, tmpl, `package main

import "fmt"

func main() {
	var keep [][]byte
	for i := 1; i <= 64; i++ {
		b := make([]byte, 16<<20)
		for k := range b {
			b[k] = byte(k)
		}
		keep = append(keep, b)
		fmt.Println("MB", i*16)
	}
	fmt.Println("ALLOCATED 1GB", len(keep))
}
`, 60*time.Second)
	if strings.Contains(out, "ALLOCATED") || exit == "exited" {
		t.Fatalf("the memory hog lived (%s):\n%s", exit, out)
	}
}

func TestNsjailTimeLimit(t *testing.T) {
	j, tmpl := jail(t, func(c *config) { c.maxLife = 3 * time.Second })
	_, exit, took := runMain(t, j, tmpl, `package main

func main() {
	for {
	}
}
`, 30*time.Second)
	if exit == "exited" || took > 15*time.Second {
		t.Fatalf("the loop ended %s after %s", exit, took)
	}
}

func TestNsjailBadImport(t *testing.T) {
	j, tmpl := jail(t, nil)
	dir := t.TempDir()
	tmpl.write(dir, map[string]string{"main.go": "package main\n\nimport _ \"github.com/google/uuid\"\n\nfunc main() {}\n"})
	var log bytes.Buffer
	start := time.Now()
	err := j.Build(context.Background(), dir, &log)
	if err == nil || time.Since(start) > 30*time.Second {
		t.Fatalf("build: %v after %s\n%s", err, time.Since(start), log.String())
	}
	if !strings.Contains(log.String(), "github.com/google/uuid") {
		t.Errorf("log:\n%s", log.String())
	}
	if ds, _ := checkFiles(map[string]string{"main.go": "package main\n\nimport _ \"github.com/google/uuid\"\n"}); len(ds) != 1 {
		t.Errorf("diagnostics %v", ds)
	}
}

func TestNsjailIdleSession(t *testing.T) {
	j, tmpl := jail(t, nil)
	cfg := loadConfig()
	cfg.runOrigin, cfg.sessions, cfg.idle, cfg.maxLife, cfg.buildTimeout = "http://*.run.test", 2, time.Second, time.Hour, time.Minute
	cfg.dir = t.TempDir()
	os.Chmod(cfg.dir, 0o755)
	s, err := newServer(cfg, j, tmpl)
	if err != nil {
		t.Fatal(err)
	}
	defer s.shutdown()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.loop(ctx)
	s.take()
	res, ss := s.run(ctx, map[string]string{"main.go": `package main

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "jailed hello") })
	http.ListenAndServe(":"+os.Getenv("PORT"), nil)
}
`}, nil, nil)
	if ss == nil {
		t.Fatalf("%+v", res)
	}
	hs := newTestHTTP(t, s)
	if code, body, _ := get(t, hs, res.URL, "/"); code != 200 || body != "jailed hello" {
		t.Fatalf("%d %q", code, body)
	}
	waitGone(t, s, res.Session, 10*time.Second)
	select {
	case <-ss.proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("the program outlived its session")
	}
}
