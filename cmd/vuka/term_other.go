//go:build !(darwin || freebsd || netbsd || openbsd || dragonfly || linux)

package main

import (
	"errors"
	"os"
)

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// makeRaw isn't done here: questions are answered a line at a time.
func makeRaw(*os.File) (func(), error) { return nil, errors.New("no raw terminal") }
