//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// ownGroup starts c in a process group of its own, which terminate and
// kill signal whole: Vite's children too.
func ownGroup(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

func terminate(c *exec.Cmd) { syscall.Kill(-c.Process.Pid, syscall.SIGTERM) }

func kill(c *exec.Cmd) { syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
