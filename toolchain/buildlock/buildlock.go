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

func path(root string) (string, bool) {
	if root == "" {
		return "", false
	}
	dir := filepath.Join(root, ".vuka")
	if os.MkdirAll(dir, 0o755) != nil {
		return "", false
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err != nil {
		os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*\n"), 0o644)
	}
	return filepath.Join(dir, file), true
}

// Hold marks the module at root as being read until the returned function is
// called. Without a lock file it holds nothing, and the module is not pruned.
func Hold(root string) func() {
	p, ok := path(root)
	if !ok {
		return func() {}
	}
	release, _ := lockShared(p)
	return release
}

// TryPrune takes the lock nothing holds; ok is false when something does.
func TryPrune(root string) (release func(), ok bool) {
	p, found := path(root)
	if !found {
		return func() {}, false
	}
	return lockTryExclusive(p)
}
