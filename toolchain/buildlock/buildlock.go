// Package buildlock, for the vuka command's use only like package load, is the lock a module's .vuka directory holds between the
// go commands vuka runs for the module, which hold it shared while they run
// and read the build cache, and the watch loop's cache pruning, which takes
// it exclusively, without waiting, so it never removes a file one of them
// may be reading.
package buildlock

import (
	"os"
	"path/filepath"
)

const file = "build.lock"

func path(root string, create bool) (p string, ok, created bool) {
	if root == "" {
		return "", false, false
	}
	dir := filepath.Join(root, ".vuka")
	if _, err := os.Stat(dir); err != nil {
		if !create || os.MkdirAll(dir, 0o755) != nil {
			return "", false, false
		}
		created = true
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err != nil {
		os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*\n"), 0o644)
	}
	return filepath.Join(dir, file), true, created
}

// Hold marks the module at root as being read until the returned function is
// called. A module without a .vuka directory holds nothing: its readers
// started before the pruning could make one, which skips that round.
func Hold(root string) func() {
	p, ok, _ := path(root, false)
	if !ok {
		return func() {}
	}
	release, _ := lockShared(p)
	return release
}

// Prepare makes the module's .vuka directory, so the readers that start
// after it hold the lock and the first pruning needn't wait a round.
func Prepare(root string) { path(root, true) }

// TryPrune takes the lock nothing holds; ok is false when something does, or
// when the .vuka directory had to be made, since readers may have started
// without it.
func TryPrune(root string) (release func(), ok bool) {
	p, found, created := path(root, true)
	if !found || created {
		return func() {}, false
	}
	return lockTryExclusive(p)
}
