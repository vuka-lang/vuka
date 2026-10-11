//go:build !unix

package main

import (
	"os"
	"os/exec"
)

func ownGroup(*exec.Cmd) {}

func terminate(c *exec.Cmd) { c.Process.Signal(os.Interrupt) }

func kill(c *exec.Cmd) { c.Process.Kill() }
