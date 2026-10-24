//go:build !unix && !windows

package main

import (
	"os"
	"os/exec"
)

func ownGroup(*exec.Cmd) {}

func terminate(c *exec.Cmd) error { return c.Process.Signal(os.Interrupt) }

func kill(c *exec.Cmd) { c.Process.Kill() }

func appGroup(*exec.Cmd) {}

func interrupt(c *exec.Cmd, sig os.Signal) error { return c.Process.Signal(sig) }

func killTree(c *exec.Cmd) { c.Process.Kill() }

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	p.Release()
	return true
}
