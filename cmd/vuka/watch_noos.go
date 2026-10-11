//go:build !(darwin || dragonfly || freebsd || netbsd || openbsd || linux)

package main

import (
	"errors"
	"io/fs"
)

func newOSWatch(func(path string, dir bool) bool) (osWatch, error) {
	return nil, errors.New("no file system events on this system")
}

func fileID(fs.FileInfo) uint64 { return 0 }
