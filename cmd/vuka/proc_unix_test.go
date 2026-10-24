//go:build unix

package main

import (
	"syscall"
	"time"
)

// endGroup kills the process group pgid and waits, a while, for it to end.
func endGroup(pgid int) {
	syscall.Kill(-pgid, syscall.SIGKILL)
	for end := time.Now().Add(10 * time.Second); syscall.Kill(-pgid, 0) == nil && time.Now().Before(end); {
		time.Sleep(10 * time.Millisecond)
	}
}
