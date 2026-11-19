package main

import (
	"os"
	"path/filepath"
)

// A build of the module holds buildLockFile shared while go runs; the watch
// loop's cache pruning takes it exclusively, without waiting, so it never
// removes a file a build of the module may be reading.
const buildLockFile = "build.lock"

func buildLockPath(root string) (string, bool) {
	dir := filepath.Join(root, ".vuka")
	if os.MkdirAll(dir, 0o755) != nil {
		return "", false
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err != nil {
		writeIfChanged(filepath.Join(dir, ".gitignore"), []byte("*\n"))
	}
	return filepath.Join(dir, buildLockFile), true
}

// holdBuild marks a build of the module's as running until the returned
// function is called.
func holdBuild(root string) func() {
	p, ok := buildLockPath(root)
	if !ok {
		return func() {}
	}
	release, _ := lockFileShared(p)
	return release
}

// tryPrune takes the lock no build holds; ok is false when one does.
func tryPrune(root string) (release func(), ok bool) {
	p, found := buildLockPath(root)
	if !found {
		return func() {}, false
	}
	return lockFileTryExclusive(p)
}
