//go:build unix

package main

import (
	"fmt"
	"os"
	"syscall"
)

// execGo replaces the shim with the real go: its signals, terminal and
// exit status are the go command's own. It returns only when the exec fails.
func execGo(real string, args, env []string) int {
	err := syscall.Exec(real, append([]string{os.Args[0]}, args...), env)
	fmt.Fprintf(os.Stderr, "vuka: the go shim runs %s: %v\n", real, err)
	return 1
}
