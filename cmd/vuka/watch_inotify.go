//go:build linux

package main

import (
	"bytes"
	"errors"
	"io/fs"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// inWatch watches with inotify: one watch per directory reports its
// entries' writes, creations, removals and renames.
type inWatch struct {
	fd, ep int
	mu     sync.Mutex
	dirs   map[string]inDir
	names  map[int32]string // a watch's directory
	filter func(path string, dir bool) bool
	c      chan struct{}
	err    chan error
	closed atomic.Bool
}

type inDir struct {
	wd int
	id uint64
}

const inMask = syscall.IN_MODIFY | syscall.IN_ATTRIB | syscall.IN_CLOSE_WRITE | syscall.IN_CREATE | syscall.IN_DELETE |
	syscall.IN_DELETE_SELF | syscall.IN_MOVED_FROM | syscall.IN_MOVED_TO | syscall.IN_MOVE_SELF | syscall.IN_ONLYDIR

func newOSWatch(filter func(path string, dir bool) bool) (osWatch, error) {
	fd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC | syscall.IN_NONBLOCK)
	if err != nil {
		return nil, err
	}
	ep, err := syscall.EpollCreate1(syscall.EPOLL_CLOEXEC)
	if err != nil {
		syscall.Close(fd)
		return nil, err
	}
	if err := syscall.EpollCtl(ep, syscall.EPOLL_CTL_ADD, fd, &syscall.EpollEvent{Events: syscall.EPOLLIN, Fd: int32(fd)}); err != nil {
		syscall.Close(fd)
		syscall.Close(ep)
		return nil, err
	}
	w := &inWatch{fd: fd, ep: ep, dirs: map[string]inDir{}, names: map[int32]string{}, filter: filter, c: make(chan struct{}, 1), err: make(chan error, 1)}
	go w.loop()
	return w, nil
}

func (w *inWatch) events() <-chan struct{} { return w.c }
func (w *inWatch) errors() <-chan error    { return w.err }

// watch covers dirs, and so their files: a directory's watch sees them. One
// replaced since it was added is added again.
func (w *inWatch) watch(dirs, files map[string]uint64) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed.Load() {
		return errors.New("closed")
	}
	for path, d := range w.dirs {
		if id, ok := dirs[path]; !ok || id != 0 && id != d.id {
			syscall.InotifyRmWatch(w.fd, uint32(d.wd)) // gone already when the directory is
			delete(w.dirs, path)
			delete(w.names, int32(d.wd))
		}
	}
	for path, id := range dirs {
		if _, ok := w.dirs[path]; ok {
			continue
		}
		wd, err := syscall.InotifyAddWatch(w.fd, path, inMask)
		if err != nil {
			if err == syscall.ENOENT || err == syscall.ENOTDIR || err == syscall.EACCES {
				continue
			}
			return &fs.PathError{Op: "inotify_add_watch", Path: path, Err: err}
		}
		w.dirs[path] = inDir{wd, id}
		w.names[int32(wd)] = path
	}
	return nil
}

func (w *inWatch) loop() {
	defer syscall.Close(w.fd)
	defer syscall.Close(w.ep)
	events := make([]syscall.EpollEvent, 1)
	buf := make([]byte, 64<<10)
	for !w.closed.Load() {
		n, err := syscall.EpollWait(w.ep, events, 250) // a timeout to notice close
		switch {
		case err == syscall.EINTR:
			continue
		case err != nil:
			w.err <- err
			return
		case n == 0:
			continue
		}
		relevant := false
		for {
			n, err := syscall.Read(w.fd, buf)
			if err != nil || n <= 0 {
				break
			}
			relevant = w.relevant(buf[:n]) || relevant
		}
		if relevant {
			select {
			case w.c <- struct{}{}:
			default:
			}
		}
	}
}

// relevant reports whether any of the events in buf concerns a directory or
// a file the filter keeps: writes to other files (an app's log, its
// database) don't make the sources be looked at.
func (w *inWatch) relevant(buf []byte) bool {
	for len(buf) >= syscall.SizeofInotifyEvent {
		ev := (*syscall.InotifyEvent)(unsafe.Pointer(&buf[0]))
		size := syscall.SizeofInotifyEvent + int(ev.Len)
		if size > len(buf) {
			return true
		}
		name := string(bytes.TrimRight(buf[syscall.SizeofInotifyEvent:size], "\x00"))
		buf = buf[size:]
		w.mu.Lock()
		dir, ok := w.names[ev.Wd]
		w.mu.Unlock()
		switch {
		case ev.Mask&syscall.IN_Q_OVERFLOW != 0, !ok, name == "", ev.Mask&syscall.IN_ISDIR != 0:
			return true
		case w.filter == nil || w.filter(filepath.Join(dir, name), false):
			return true
		}
	}
	return false
}

func (w *inWatch) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed.Store(true)
}

// fileID is the identity of the file fi describes: its inode.
func fileID(fi fs.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return st.Ino
	}
	return 0
}
