package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestExeSuffix(t *testing.T) {
	if got := goosExeSuffix("windows"); got != ".exe" {
		t.Errorf("windows: %q", got)
	}
	for _, goos := range []string{"linux", "darwin", "freebsd"} {
		if got := goosExeSuffix(goos); got != "" {
			t.Errorf("%s: %q", goos, got)
		}
	}
	if got, want := exe(filepath.Join("d", "app")), filepath.Join("d", "app")+exeSuffix(); got != want {
		t.Errorf("exe: %q, want %q", got, want)
	}
}

// TestNodeBin: on Windows, node_modules/.bin's command is its .cmd shim (or
// bun's .exe); the extensionless file is a shell script there.
func TestNodeBin(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "node_modules", ".bin", "vite")
	for _, goos := range []string{"linux", "windows"} {
		if got := goosNodeBin(goos, dir, "vite"); got != base {
			t.Errorf("%s, none installed: %q, want %q", goos, got, base)
		}
	}
	writeFile(t, base, "#!/bin/sh\n")
	writeFile(t, base+".exe", "")
	if got := goosNodeBin("windows", dir, "vite"); got != base+".exe" {
		t.Errorf("windows, bun's shim: %q", got)
	}
	writeFile(t, base+".cmd", "@node vite.js %*\n")
	if got := goosNodeBin("windows", dir, "vite"); got != base+".cmd" {
		t.Errorf("windows: %q, want the .cmd shim", got)
	}
	if got := goosNodeBin("linux", dir, "vite"); got != base {
		t.Errorf("linux: %q", got)
	}
}

func TestTailwindInstallHint(t *testing.T) {
	for _, c := range []struct{ goos, goarch, has, hasnt string }{
		{"darwin", "arm64", "brew install tailwindcss", ""},
		{"windows", "amd64", "tailwindcss-windows-x64.exe", "brew"},
		{"windows", "arm64", "tailwindcss-windows-arm64.exe", "brew"},
		{"linux", "amd64", "tailwindcss-linux-x64 ", "brew"},
	} {
		hint := tailwindInstallHint(c.goos, c.goarch)
		if !strings.Contains(hint, c.has) || !strings.Contains(hint, "npm i -D @tailwindcss/cli") || c.hasnt != "" && strings.Contains(hint, c.hasnt) {
			t.Errorf("%s/%s: %s", c.goos, c.goarch, hint)
		}
	}
}

// TestTailwindStandalone: the standalone CLI is found on PATH under the name
// it is released as.
func TestTailwindStandalone(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	name := standaloneTailwind(runtime.GOOS, runtime.GOARCH)
	if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := tailwindBinary(tempModule(t, map[string]string{"go.mod": webMod})); err != nil || filepath.Base(got) != name {
		t.Errorf("%q %v, want %s", got, err, name)
	}
}

func TestFileURIPath(t *testing.T) {
	for _, c := range []struct{ goos, path, uri string }{
		{"windows", `C:\Users\me\app\main.vuka`, "/C:/Users/me/app/main.vuka"},
		{"windows", `\\server\share\x`, "//server/share/x"},
		{"linux", "/home/me/app/main.vuka", "/home/me/app/main.vuka"},
	} {
		if got := fileURIPath(c.goos, c.path); got != c.uri {
			t.Errorf("%s %s: %s, want %s", c.goos, c.path, got, c.uri)
		}
		if c.goos == runtime.GOOS || c.goos == "windows" && !strings.HasPrefix(c.path, `\\`) {
			if got := uriOSPath(c.goos, c.uri); got != c.path {
				t.Errorf("%s %s: back %s, want %s", c.goos, c.uri, got, c.path)
			}
		}
	}
	p := filepath.Join(t.TempDir(), "main.vuka")
	if got := uriToPath(pathToURI(p)); got != p {
		t.Errorf("round trip: %s → %s → %s", p, pathToURI(p), got)
	}
	if runtime.GOOS == "windows" && !strings.HasPrefix(pathToURI(p), "file:///") {
		t.Errorf("%s: %s", p, pathToURI(p))
	}
}

func TestProcessAlive(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Error("this process isn't alive")
	}
	c := exec.Command("go", "version")
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	if processAlive(c.Process.Pid) {
		t.Errorf("go version (%d) is alive after it exited", c.Process.Pid)
	}
}

const interruptApp = `package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	fmt.Println("ready")
	<-sig
	os.WriteFile(os.Args[1], []byte("stopped"), 0o644)
}
`

// TestInterrupt: an app started for interrupt gets the request to stop as a
// signal it can handle (on Windows, a CTRL_BREAK_EVENT: os.Interrupt), and
// stops as it does.
func TestInterrupt(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/i\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(dir, "main.go"), interruptApp)
	bin := exe(filepath.Join(dir, "app"))
	b := exec.Command("go", "build", "-o", bin, ".")
	b.Dir, b.Env = dir, append(os.Environ(), "GOWORK=off")
	if out, err := b.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	mark := filepath.Join(dir, "mark")
	c := exec.Command(bin, mark)
	out, _ := c.StdoutPipe()
	appGroup(c)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	out.Read(buf) // "ready": its handler is in place
	a := &app{cmd: c, done: make(chan struct{})}
	go func() { a.err = c.Wait(); close(a.done) }()
	start := time.Now()
	a.stop(syscall.SIGTERM, 10*time.Second, nil)
	if b, _ := os.ReadFile(mark); string(b) != "stopped" || a.err != nil {
		t.Errorf("the app didn't stop on its own (%v after %v): %q", a.err, time.Since(start), b)
	}
}

// TestRemoveAll: a directory whose binary has just run is removed.
func TestRemoveAll(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "1")
	writeFile(t, filepath.Join(dir, "sub", "f"), "x")
	if err := removeAll(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("%s is still there: %v", dir, err)
	}
}
