//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package main

import (
	"errors"
	"io/fs"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// kqWatch watches with kqueue: a vnode event on each directory (an entry
// added, removed or renamed) and on each file (written, replaced, removed).
type kqWatch struct {
	kq     int
	mu     sync.Mutex
	open   map[string]kqFile
	c      chan struct{}
	err    chan error
	closed atomic.Bool
}

type kqFile struct {
	fd int
	id uint64
}

// newOSWatch starts a watch. Its events come from the directories' entries
// and the watched files themselves, so the filter isn't needed.
func newOSWatch(filter func(path string, dir bool) bool) (osWatch, error) {
	kq, err := syscall.Kqueue()
	if err != nil {
		return nil, err
	}
	syscall.CloseOnExec(kq)
	w := &kqWatch{kq: kq, open: map[string]kqFile{}, c: make(chan struct{}, 1), err: make(chan error, 1)}
	go w.loop()
	return w, nil
}

func (w *kqWatch) events() <-chan struct{} { return w.c }
func (w *kqWatch) errors() <-chan error    { return w.err }

// watch covers exactly dirs and files (by identity): one replaced since it
// was opened is opened again.
func (w *kqWatch) watch(dirs, files map[string]uint64) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed.Load() {
		return errors.New("closed")
	}
	paths := make(map[string]uint64, len(dirs)+len(files))
	for p, id := range dirs {
		paths[p] = id
	}
	for p, id := range files {
		paths[p] = id
	}
	for path, f := range w.open {
		if id, ok := paths[path]; !ok || id != 0 && id != f.id {
			syscall.Close(f.fd) // closing it drops its event
			delete(w.open, path)
		}
	}
	flags := syscall.O_RDONLY | syscall.O_NONBLOCK | syscall.O_CLOEXEC
	if runtime.GOOS == "darwin" {
		flags = 0x8000 | syscall.O_CLOEXEC // O_EVTONLY: for events only; doesn't hold the volume
	}
	for path := range paths {
		if _, ok := w.open[path]; ok {
			continue
		}
		fd, err := syscall.Open(path, flags, 0)
		if err != nil {
			if err == syscall.ENOENT || err == syscall.ENOTDIR || err == syscall.EACCES {
				continue // gone, or not ours to read: a later look sees it
			}
			return &fs.PathError{Op: "open", Path: path, Err: err}
		}
		var st syscall.Stat_t
		if err := syscall.Fstat(fd, &st); err != nil {
			syscall.Close(fd)
			continue
		}
		var ev syscall.Kevent_t
		syscall.SetKevent(&ev, fd, syscall.EVFILT_VNODE, syscall.EV_ADD|syscall.EV_ENABLE|syscall.EV_CLEAR)
		ev.Fflags = syscall.NOTE_DELETE | syscall.NOTE_WRITE | syscall.NOTE_EXTEND | syscall.NOTE_ATTRIB | syscall.NOTE_RENAME | syscall.NOTE_REVOKE
		if _, err := syscall.Kevent(w.kq, []syscall.Kevent_t{ev}, nil, nil); err != nil {
			syscall.Close(fd)
			return &fs.PathError{Op: "kevent", Path: path, Err: err}
		}
		w.open[path] = kqFile{fd, uint64(st.Ino)}
	}
	return nil
}

func (w *kqWatch) loop() {
	defer syscall.Close(w.kq)
	events := make([]syscall.Kevent_t, 64)
	timeout := syscall.NsecToTimespec(int64(250 * time.Millisecond)) // to notice close
	for !w.closed.Load() {
		n, err := syscall.Kevent(w.kq, nil, events, &timeout)
		switch {
		case err == syscall.EINTR:
		case err != nil:
			w.err <- err
			return
		case n > 0:
			select {
			case w.c <- struct{}{}:
			default:
			}
		}
	}
}

func (w *kqWatch) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed.Swap(true) {
		return
	}
	for _, f := range w.open {
		syscall.Close(f.fd)
	}
	w.open = nil
}

// fileID is the identity of the file fi describes: its inode.
func fileID(fi fs.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino)
	}
	return 0
}
