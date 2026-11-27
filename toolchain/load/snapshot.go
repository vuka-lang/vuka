package load

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// A Snapshot is the module's tree as one build reads it. A build reads the
// module from many places — discovery, folder routes, assets, the context
// processors, auto-linking, the build's keys — and each would list each
// directory, and stat and read each source, again; through a Snapshot each
// is listed, statted and read once, the first time one of them asks, and
// every later ask is answered from memory. A Snapshot is for one build: what
// changes on disk after it first read a path it doesn't see. A nil Snapshot
// reads the disk, as the package's functions do.
type Snapshot struct {
	mu     sync.Mutex
	dirs   map[string]snapDir
	files  map[string]snapFile
	stats  map[string]snapStat
	lstats map[string]snapStat
}

type snapDir struct {
	entries []fs.DirEntry
	err     error
}

type snapFile struct {
	data []byte
	err  error
}

type snapStat struct {
	fi  fs.FileInfo
	err error
}

// NewSnapshot is an empty Snapshot: it reads each path once it is asked for.
func NewSnapshot() *Snapshot {
	return &Snapshot{dirs: map[string]snapDir{}, files: map[string]snapFile{}, stats: map[string]snapStat{}, lstats: map[string]snapStat{}}
}

// ReadDir is ReadDir, the directory listed once. An entry's Info is the
// snapshot's Lstat of it.
func (s *Snapshot) ReadDir(dir string) ([]fs.DirEntry, error) {
	if s == nil {
		return ReadDir(dir)
	}
	key := filepath.Clean(dir)
	s.mu.Lock()
	d, ok := s.dirs[key]
	s.mu.Unlock()
	if !ok {
		entries, err := ReadDir(key)
		for i, e := range entries {
			entries[i] = snapEntry{e, s, filepath.Join(key, e.Name())}
		}
		d = snapDir{entries, err}
		s.mu.Lock()
		s.dirs[key] = d
		s.mu.Unlock()
	}
	return slices.Clone(d.entries), d.err
}

// ReadSource is ReadSource, the file read once.
func (s *Snapshot) ReadSource(path string) ([]byte, error) {
	if s == nil {
		return ReadSource(path)
	}
	key := filepath.Clean(path)
	s.mu.Lock()
	f, ok := s.files[key]
	s.mu.Unlock()
	if !ok {
		data, err := ReadSource(key)
		f = snapFile{data, err}
		s.mu.Lock()
		s.files[key] = f
		s.mu.Unlock()
	}
	return slices.Clone(f.data), f.err
}

// ReadFile is os.ReadFile through the snapshot: ReadSource when there is one.
func (s *Snapshot) ReadFile(path string) ([]byte, error) {
	if s == nil {
		return os.ReadFile(path)
	}
	return s.ReadSource(path)
}

// Stat is os.Stat, the path statted once.
func (s *Snapshot) Stat(path string) (fs.FileInfo, error) {
	if s == nil {
		return os.Stat(path)
	}
	return s.stat(s.stats, path, os.Stat)
}

// Lstat is os.Lstat, the path statted once.
func (s *Snapshot) Lstat(path string) (fs.FileInfo, error) {
	if s == nil {
		return os.Lstat(path)
	}
	return s.stat(s.lstats, path, os.Lstat)
}

func (s *Snapshot) stat(m map[string]snapStat, path string, stat func(string) (fs.FileInfo, error)) (fs.FileInfo, error) {
	key := filepath.Clean(path)
	s.mu.Lock()
	st, ok := m[key]
	s.mu.Unlock()
	if !ok {
		fi, err := stat(key)
		st = snapStat{fi, err}
		s.mu.Lock()
		m[key] = st
		s.mu.Unlock()
	}
	return st.fi, st.err
}

// Exists reports whether path names something os.Stat finds, answered from
// its directory's listing when it holds no symlink by that name.
func (s *Snapshot) Exists(path string) bool {
	if s == nil {
		_, err := os.Stat(path)
		return err == nil
	}
	dir, name := filepath.Split(filepath.Clean(path))
	if entries, err := s.ReadDir(dir); err == nil && name != "" {
		i := slices.IndexFunc(entries, func(e fs.DirEntry) bool { return e.Name() == name })
		if i < 0 {
			return false
		}
		if entries[i].Type()&fs.ModeSymlink == 0 {
			return true
		}
	}
	_, err := s.Stat(path)
	return err == nil
}

// IsDir reports whether path is a directory, following symlinks.
func (s *Snapshot) IsDir(path string) bool {
	fi, err := s.Stat(path)
	return err == nil && fi.IsDir()
}

// HasGoMod is HasGoMod through the snapshot.
func (s *Snapshot) HasGoMod(dir string) bool {
	entries, err := s.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Name() == "go.mod" {
			return !e.IsDir()
		}
	}
	return false
}

// WalkDir is WalkDir through the snapshot.
func (s *Snapshot) WalkDir(root string, fn fs.WalkDirFunc) error {
	if s == nil {
		return WalkDir(root, fn)
	}
	info, err := s.Lstat(root)
	if err != nil {
		err = fn(root, nil, err)
	} else {
		err = walkDirIn(s, root, snapEntry{fs.FileInfoToDirEntry(info), s, filepath.Clean(root)}, fn)
	}
	if err == filepath.SkipDir || err == filepath.SkipAll {
		return nil
	}
	return err
}

// Forget drops what the snapshot read of each path, and the listings of the
// directories above it: the next ask reads them again. A build forgets the
// files it writes in the module.
func (s *Snapshot) Forget(paths ...string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range paths {
		p = filepath.Clean(p)
		delete(s.files, p)
		for {
			delete(s.dirs, p)
			delete(s.stats, p)
			delete(s.lstats, p)
			parent := filepath.Dir(p)
			if parent == p {
				break
			}
			p = parent
		}
	}
}

// Discover is Discover reading through the snapshot.
func (s *Snapshot) Discover(root, modPath, dir string, recursive bool) ([]*Package, error) {
	return discover(s, root, modPath, dir, recursive, s.ReadSource)
}

// AutoLink is AutoLink reading through the snapshot.
func (s *Snapshot) AutoLink(root, modPath string, pkgs []*Package, gens []Generated) ([]Generated, []LinkedMain, error) {
	return autoLink(s, root, modPath, pkgs, gens)
}

// RestartKeys is RestartKeys reading through the snapshot.
func (s *Snapshot) RestartKeys(root, modPath string, pkgs []*Package, gens []Generated) ([]Generated, RestartReport, error) {
	return restartKeys(s, root, modPath, pkgs, gens)
}

// A snapEntry is a directory entry whose Info is the snapshot's Lstat.
type snapEntry struct {
	fs.DirEntry
	s    *Snapshot
	path string
}

func (e snapEntry) Info() (fs.FileInfo, error) { return e.s.Lstat(e.path) }
