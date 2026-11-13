//go:build unix

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestOrphanVites(t *testing.T) {
	t.Parallel()
	const bin = "/app/frontend/node_modules/.bin/vite"
	ps := strings.Join([]string{
		"  101     1   101 node " + bin,
		"  102   900   102 node " + bin,                                 // its vuka is running
		"  103     1   900 node " + bin,                                 // not a group of its own
		"  104     1   104 node /other/frontend/node_modules/.bin/vite", // another project's
		"  105     1   105 node " + bin + " build",                      // still names the binary
		"  106     1   106 vim " + bin + ".txt",                         // not the binary
		"nonsense",
	}, "\n")
	if got, want := orphanVites(ps, bin), []int{101, 105}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestStopOrphanVites(t *testing.T) {
	t.Parallel()
	if bin := os.Getenv("VUKA_TEST_ORPHAN_VITE"); bin != "" {
		c := exec.Command(bin)
		c.Dir = filepath.Dir(filepath.Dir(filepath.Dir(bin)))
		ownGroup(c)
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		fmt.Printf("orphan=%d\n", c.Process.Pid)
		os.Exit(0) // leaves Vite behind, as a killed vuka run does
	}
	root := tempModule(t, map[string]string{"go.mod": "module x\n", "frontend/package.json": "{}"})
	dir := filepath.Join(root, "frontend")
	fakeVite(t, dir)
	bin := viteBinary(dir)

	helper := exec.Command(os.Args[0], "-test.run=^TestStopOrphanVites$")
	helper.Env = append(os.Environ(), "VUKA_TEST_ORPHAN_VITE="+bin)
	out, err := helper.Output()
	if err != nil {
		t.Fatal(err)
	}
	_, after, ok := strings.Cut(string(out), "orphan=")
	if !ok {
		t.Fatalf("no orphan: %s", out)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(after, "\n", 2)[0]))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { kill(&exec.Cmd{Process: &os.Process{Pid: pid}}) })

	owned := exec.Command(bin)
	owned.Dir = dir
	ownGroup(owned)
	if err := owned.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { kill(owned); owned.Wait() })

	deadline := time.Now().Add(3 * time.Second)
	for {
		list, err := exec.Command("ps", "-axo", "pid=,ppid=,pgid=,command=").Output()
		if err != nil {
			t.Skip("no ps:", err)
		}
		if slices.Contains(orphanVites(string(list), bin), pid) {
			break
		}
		if time.Now().After(deadline) {
			t.Skip("an orphan's parent isn't pid 1 here")
		}
		time.Sleep(20 * time.Millisecond)
	}
	stopOrphanVites(bin)
	if processAlive(pid) {
		t.Error("the orphan is still running")
	}
	if !processAlive(owned.Process.Pid) {
		t.Error("a Vite with a live parent was stopped")
	}
}
