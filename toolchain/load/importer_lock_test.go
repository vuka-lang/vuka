package load

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vuka-lang/vuka/v2/toolchain/buildlock"
)

func lockModule(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("no file locks")
	}
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOWORK", "off")
	t.Setenv("GOCACHE", t.TempDir())
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "go.mod"), "module shop\n\ngo 1.23\n")
	write(t, filepath.Join(root, "a", "a.go"), "package a\n\nfunc A() int { return 1 }\n")
	return root
}

func pruneRefused(root string) bool {
	release, ok := buildlock.TryPrune(root)
	if ok {
		release()
	}
	return !ok
}

func TestImporterHoldsBuildLockWhileReading(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	root := lockModule(t)
	im := NewImporter(root, "")
	rc, err := im.lookup("shop/a")
	if err != nil {
		t.Fatal(err)
	}
	if !pruneRefused(root) {
		t.Error("the cache could be pruned while the export data is open")
	}
	rc.Close()
	if pruneRefused(root) {
		t.Error("the importer kept the lock after the read")
	}
	if _, err := im.lookup("shop/nowhere"); err == nil {
		t.Fatal("no error for a missing package")
	}
	if pruneRefused(root) {
		t.Error("a failed lookup kept the lock")
	}
}

func TestImporterHoldsBuildLockWhileListing(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	root := lockModule(t)
	real, err := exec.LookPath("go")
	if err != nil {
		t.Skip(err)
	}
	bin := t.TempDir()
	started := filepath.Join(bin, "started")
	shim := "#!/bin/sh\ntouch " + started + "\nsleep 2\nexec " + real + " \"$@\"\n"
	write(t, filepath.Join(bin, "go"), shim)
	if err := os.Chmod(filepath.Join(bin, "go"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	im := NewImporter(root, "")
	done := make(chan error, 1)
	go func() { done <- im.Prefetch([]string{"shop/a"}) }()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("go list never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !pruneRefused(root) {
		t.Error("the cache could be pruned while go list ran")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if pruneRefused(root) {
		t.Error("the importer kept the lock after go list")
	}
}

func TestImporterRelistsPrunedArchive(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	root := lockModule(t)
	im := NewImporter(root, "")
	rc, err := im.lookup("shop/a")
	if err != nil {
		t.Fatal(err)
	}
	rc.Close()
	file := im.exports["shop/a"]
	if file == "" || !strings.HasPrefix(file, os.Getenv("GOCACHE")) {
		t.Fatalf("export data %q is not in the cache", file)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	rc, err = im.lookup("shop/a")
	if err != nil {
		t.Fatalf("a pruned archive isn't rebuilt: %v", err)
	}
	rc.Close()
}
