package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"

	"golang.org/x/sys/windows"
)

// Windows has no signals but Kill: a console program is asked to stop with
// a CTRL_BREAK_EVENT, which a Go program sees as os.Interrupt and Node as
// SIGBREAK. The event goes to a process group, so what is asked that way is
// started in a group of its own (CREATE_NEW_PROCESS_GROUP), which a
// console's Ctrl-C doesn't reach: vuka passes it on.

func newGroup(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.CreationFlags |= windows.CREATE_NEW_PROCESS_GROUP
}

// ownGroup starts c in a process group of its own, which terminate asks to
// stop and kill ends whole: Vite under its .cmd shim too.
func ownGroup(c *exec.Cmd) { newGroup(c) }

func terminate(c *exec.Cmd) error {
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(c.Process.Pid))
}

func kill(c *exec.Cmd) { killTree(c) }

// appGroup readies c, an app vuka run -watch runs, for interrupt: a group
// of its own, so that it alone gets the CTRL_BREAK_EVENT.
func appGroup(c *exec.Cmd) { newGroup(c) }

// interrupt asks the process c started (in a group of its own: appGroup,
// ownGroup) to stop. Kill kills it; any other signal is a CTRL_BREAK_EVENT.
// It fails when vuka has no console to send the event through.
func interrupt(c *exec.Cmd, sig os.Signal) error {
	if sig == os.Kill {
		return c.Process.Kill()
	}
	return terminate(c)
}

// killTree kills the process c started and those it started: a .cmd shim's
// Node, an app's children.
func killTree(c *exec.Cmd) {
	taskkill := filepath.Join(os.Getenv("SystemRoot"), "System32", "taskkill.exe")
	if _, err := os.Stat(taskkill); err != nil {
		taskkill = "taskkill"
	}
	exec.Command(taskkill, "/T", "/F", "/PID", strconv.Itoa(c.Process.Pid)).Run()
	c.Process.Kill()
}

// processAlive reports whether the process pid is running.
func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	const stillActive = 259
	return windows.GetExitCodeProcess(h, &code) == nil && code == stillActive
}
