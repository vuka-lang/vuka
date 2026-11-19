//go:build unix

package buildlock

import (
	"os"
	"syscall"
)

func flockFile(path string, how int) (func(), bool) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return func() {}, false
	}
	if syscall.Flock(int(f.Fd()), how) != nil {
		f.Close()
		return func() {}, false
	}
	return func() { f.Close() }, true
}

func lockShared(path string) (func(), bool) { return flockFile(path, syscall.LOCK_SH) }

func lockTryExclusive(path string) (func(), bool) {
	return flockFile(path, syscall.LOCK_EX|syscall.LOCK_NB)
}
