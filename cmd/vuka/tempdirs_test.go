package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// deadPID is the pid of a process that has ended.
func deadPID(t *testing.T) int {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^$")
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	return c.Process.Pid
}

func markedDir(t *testing.T, tmp, name string, pid int, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(tmp, name)
	if err := os.MkdirAll(filepath.Join(dir, "go-build1", "b001"), 0o755); err != nil {
		t.Fatal(err)
	}
	mark := filepath.Join(dir, tempMark)
	if err := os.WriteFile(mark, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-age)
	os.Chtimes(mark, old, old)
	return dir
}

// TestSweepTemp: a vuka removes the directories an earlier vuka left, those
// it marked whose process is gone and that are a while old, and nothing else
// in the temporary directory.
func TestSweepTemp(t *testing.T) {
	tmp := t.TempDir()
	dead := deadPID(t)
	gone := map[string]string{
		"vuka-gone":         markedDir(t, tmp, "vuka-gone", dead, 3*tempStale),
		"vuka-watch-gone":   markedDir(t, tmp, "vuka-watch-gone", dead, 3*tempStale),
		"vuka-session-gone": markedDir(t, tmp, "vuka-session-gone", dead, 3*tempStale),
	}
	kept := map[string]string{
		"vuka-young":   markedDir(t, tmp, "vuka-young", dead, time.Minute),
		"vuka-running": markedDir(t, tmp, "vuka-running", os.Getppid(), 3*tempStale),
		"vuka-self":    markedDir(t, tmp, "vuka-self", os.Getpid(), 3*tempStale),
		"other-tool":   markedDir(t, tmp, "other-tool", dead, 3*tempStale),
		"vuka-nomark":  filepath.Join(tmp, "vuka-nomark"),
		"go-build123":  filepath.Join(tmp, "go-build123"),
		"vuka-badmark": markedDir(t, tmp, "vuka-badmark", 0, 3*tempStale),
	}
	os.MkdirAll(kept["vuka-nomark"], 0o755)
	os.MkdirAll(kept["go-build123"], 0o755)
	os.WriteFile(filepath.Join(kept["vuka-badmark"], tempMark), []byte("not a pid"), 0o644)
	os.WriteFile(filepath.Join(tmp, "vuka-file"), nil, 0o644)

	removed := sweepTemp(tmp, time.Now())
	if len(removed) != len(gone) {
		t.Errorf("removed %v, want %d directories", removed, len(gone))
	}
	for name, dir := range gone {
		if _, err := os.Stat(dir); err == nil {
			t.Errorf("%s is still there", name)
		}
	}
	for name, dir := range kept {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("%s was removed", name)
		}
	}
	if _, err := os.Stat(filepath.Join(tmp, "vuka-file")); err != nil {
		t.Error("a file was removed")
	}
}

// TestSessionTemp: the go commands vuka starts work in a marked directory,
// which goes with vuka; a GOTMPDIR the user set is theirs.
func TestSessionTemp(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("GOTMPDIR", "")
	os.Unsetenv("GOTMPDIR")
	done := sessionTemp()
	dir := os.Getenv("GOTMPDIR")
	if filepath.Dir(dir) != tmp {
		t.Fatalf("GOTMPDIR is %q, want a directory of %s", dir, tmp)
	}
	if pid, ok := ownedTemp(dir); !ok || pid != os.Getpid() {
		t.Errorf("the session directory isn't marked: %d %v", pid, ok)
	}
	done()
	if _, err := os.Stat(dir); err == nil {
		t.Error("the session directory is still there")
	}
	if v, ok := os.LookupEnv("GOTMPDIR"); ok {
		t.Errorf("GOTMPDIR is %q after", v)
	}

	own := t.TempDir()
	t.Setenv("GOTMPDIR", own)
	sessionTemp()()
	if got := os.Getenv("GOTMPDIR"); got != own {
		t.Errorf("GOTMPDIR is %q, want the user's %q", got, own)
	}
	if entries, _ := os.ReadDir(tmp); len(entries) > 0 {
		t.Errorf("vuka made %d entries beside the user's GOTMPDIR", len(entries))
	}
}
