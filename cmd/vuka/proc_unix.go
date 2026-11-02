//go:build unix

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ownGroup starts c in a process group of its own, which terminate and
// kill signal whole: Vite's children too.
func ownGroup(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

func terminate(c *exec.Cmd) error { return syscall.Kill(-c.Process.Pid, syscall.SIGTERM) }

func kill(c *exec.Cmd) { syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }

// appGroup readies c, an app vuka run -watch runs, for interrupt: here it
// stays in vuka's process group, so a terminal's Ctrl-C reaches it as well.
func appGroup(*exec.Cmd) {}

// interrupt asks the process c started to stop: sig.
func interrupt(c *exec.Cmd, sig os.Signal) error { return c.Process.Signal(sig) }

// killTree kills the process c started (on Windows, with what it started).
func killTree(c *exec.Cmd) { c.Process.Kill() }

// processAlive reports whether the process pid is running.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	defer p.Release()
	return !errors.Is(p.Signal(syscall.Signal(0)), os.ErrProcessDone)
}

// stopOrphanVites stops the dev servers an earlier vuka run left behind when
// it was killed or its terminal closed, so they no longer hold their ports: a
// process running this frontend's Vite (bin), whose parent has gone, and that
// still leads its own group as ownGroup started it. A Vite another vuka run
// supervises has its parent, and is left alone.
func stopOrphanVites(bin string) {
	abs, err := filepath.Abs(bin)
	if err != nil {
		return
	}
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,pgid=,command=").Output()
	if err != nil {
		return
	}
	pids := orphanVites(string(out), abs)
	for _, pid := range pids {
		syscall.Kill(-pid, syscall.SIGTERM)
	}
	deadline := time.Now().Add(2 * time.Second)
	for _, pid := range pids {
		for processAlive(pid) && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if processAlive(pid) {
			syscall.Kill(-pid, syscall.SIGKILL)
		}
	}
}

// orphanVites reads ps's "pid ppid pgid command" lines for the orphaned group
// leaders running bin.
func orphanVites(ps, bin string) []int {
	var out []int
	for _, line := range strings.Split(ps, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[1] != "1" || f[0] != f[2] || !slices.Contains(f[3:], bin) {
			continue
		}
		if pid, err := strconv.Atoi(f[0]); err == nil {
			out = append(out, pid)
		}
	}
	return out
}
