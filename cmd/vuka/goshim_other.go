//go:build !unix

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
)

// execGo runs the real go with the shim's standard streams and returns its
// exit status. Windows has no exec: the go command runs as a child. Ctrl-C
// reaches it from the console itself; the shim ignores the interrupt and
// waits for go to exit, so go decides how to stop. (Notify, not Ignore:
// the shim's handling only, nothing a child could inherit.)
func execGo(real string, args, env []string) int {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)
	c := exec.Command(real, args...)
	c.Env = env
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := c.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "vuka: the go shim runs %s: %v\n", real, err)
		return 1
	}
	return 0
}
