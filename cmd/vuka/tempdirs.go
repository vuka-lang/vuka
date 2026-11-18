package main

import (
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// The directories vuka makes in the temporary directory hold a mark naming
// its process. They are removed when vuka ends, whether it ends in an error
// or on an interrupt; one left by a vuka that was killed is removed by the
// next vuka, once its process is gone and the directory is a while old.
// The go commands vuka starts keep their go-build directories in the session
// directory (GOTMPDIR), so those go with it. Directories of other tools,
// and of vuka before the mark, are never touched.

const (
	tempPrefix = "vuka-"
	tempMark   = ".vuka-tmp"
	tempStale  = 2 * time.Hour
)

var (
	tempMu   sync.Mutex
	tempDirs = map[string]bool{}
	// signalsOwned is set once a go command is running and runGo forwards
	// signals to it; before that an interrupt removes the directories and ends.
	signalsOwned atomic.Bool
)

// mkTemp makes a marked directory in the temporary directory.
func mkTemp(pattern string) (string, error) {
	dir, err := os.MkdirTemp("", pattern)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, tempMark), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	tempMu.Lock()
	tempDirs[dir] = true
	tempMu.Unlock()
	return dir, nil
}

// removeTemp removes a directory mkTemp made.
func removeTemp(dir string) {
	os.RemoveAll(dir)
	tempMu.Lock()
	delete(tempDirs, dir)
	tempMu.Unlock()
}

// removeTemps removes every directory this vuka made and still has.
func removeTemps() {
	tempMu.Lock()
	dirs := tempDirs
	tempDirs = map[string]bool{}
	tempMu.Unlock()
	for d := range dirs {
		os.RemoveAll(d)
	}
}

// sessionTemp makes the directory the go commands of this vuka work in,
// GOTMPDIR for them unless the user set it, and returns the function that
// removes it.
func sessionTemp() (done func()) {
	if os.Getenv("GOTMPDIR") != "" {
		return func() {}
	}
	dir, err := mkTemp(tempPrefix + "session-")
	if err != nil {
		return func() {}
	}
	os.Setenv("GOTMPDIR", dir)
	return func() {
		os.Unsetenv("GOTMPDIR")
		removeTemp(dir)
	}
}

// ownedTemp reports whether dir is one vuka made: its name, and a mark
// naming a process, returned.
func ownedTemp(dir string) (pid int, ok bool) {
	if !strings.HasPrefix(filepath.Base(dir), tempPrefix) {
		return 0, false
	}
	b, err := os.ReadFile(filepath.Join(dir, tempMark))
	if err != nil {
		return 0, false
	}
	pid, err = strconv.Atoi(strings.TrimSpace(string(b)))
	return pid, err == nil && pid > 0
}

// sweepTemp removes the directories of tmp that a vuka left behind: marked,
// their process gone, and older than tempStale.
func sweepTemp(tmp string, now time.Time) (removed []string) {
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(tmp, e.Name())
		pid, ok := ownedTemp(dir)
		if !ok || pid == os.Getpid() || processAlive(pid) {
			continue
		}
		fi, err := os.Stat(filepath.Join(dir, tempMark))
		if err != nil || now.Sub(fi.ModTime()) < tempStale {
			continue
		}
		if os.RemoveAll(dir) == nil {
			removed = append(removed, dir)
		}
	}
	return removed
}

// interruptible makes an interrupt or termination of vuka remove its
// directories while no go command is running to forward it to; it returns
// the function that stops that.
func interruptible() (stop func()) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		for range sig {
			if signalsOwned.Load() {
				continue
			}
			removeTemps()
			os.Exit(130)
		}
	}()
	return func() {
		signal.Stop(sig)
		close(sig)
	}
}
