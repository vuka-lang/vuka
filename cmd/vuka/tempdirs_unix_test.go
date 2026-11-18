//go:build unix

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestInterruptedTemp: an interrupted vuka test leaves no vuka- or go-build
// directory in the temporary directory.
func TestInterruptedTemp(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	vuka := buildVuka(t)
	root := t.TempDir()
	started := filepath.Join(root, "started")
	files := map[string]string{
		"go.mod":       "module example.com/slow\n\ngo 1.25.0\n",
		"slow.go":      "package slow\n",
		"slow_test.go": "package slow\n\nimport (\n\t\"os\"\n\t\"testing\"\n\t\"time\"\n)\n\nfunc TestSlow(t *testing.T) {\n\tos.WriteFile(os.Getenv(\"STARTED\"), nil, 0o644)\n\ttime.Sleep(20 * time.Second)\n}\n",
	}
	for name, src := range files {
		writeFile(t, filepath.Join(root, name), src)
	}
	tmp, _ := filepath.EvalSymlinks(t.TempDir())
	c := exec.Command(vuka, "test", "./...")
	c.Dir = root
	c.Env = append(os.Environ(), "TMPDIR="+tmp, "STARTED="+started, "VUKA_GOCACHE_MAX=0", "GOFLAGS=-mod=mod")
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- c.Wait() }()
	deadline := time.Now().Add(2 * time.Minute)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			c.Process.Kill()
			t.Fatal("the test never started")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Not the group: only vuka is told, as a kill of its pid does.
	c.Process.Signal(syscall.SIGINT)
	select {
	case <-exited:
	case <-time.After(time.Minute):
		c.Process.Kill()
		t.Fatal("vuka didn't end")
	}
	time.Sleep(200 * time.Millisecond)
	if entries, _ := os.ReadDir(tmp); len(entries) > 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("left in the temporary directory: %v", names)
	}
}
