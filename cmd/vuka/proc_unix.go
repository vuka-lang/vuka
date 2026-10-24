//go:build unix

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
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
