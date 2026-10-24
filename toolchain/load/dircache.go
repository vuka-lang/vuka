package load

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// A build reads the module's directories from many places: discovery, the
// context processors, folder routes, assets, auto-linking, the build/
// mirror. ReadDir keeps each directory's entries while the directory is
// unchanged on disk (its modification time, which an entry added, removed
// or renamed moves), so a build reads each directory once, however many of
// them ask; a later ask costs one stat. Entries are names and types; Info
// reads the file when asked, as os.ReadDir's do.

type dirEntries struct {
	mod     time.Time
	entries []fs.DirEntry
}

var (
	dirs     sync.Map // cleaned path → dirEntries
	dirReads atomic.Int64
)

// ReadDir is os.ReadDir, read from disk only when the directory changed
// since it last was.
func ReadDir(dir string) ([]fs.DirEntry, error) {
	key := filepath.Clean(dir)
	fi, err := os.Stat(key)
	if err != nil {
		dirs.Delete(key)
		return nil, err
	}
	if c, ok := dirs.Load(key); ok {
		if c := c.(dirEntries); c.mod.Equal(fi.ModTime()) {
			return slices.Clone(c.entries), nil
		}
	}
	dirReads.Add(1)
	entries, err := os.ReadDir(key)
	if err != nil {
		return entries, err
	}
	// A directory changed within the clock's resolution of being read
	// may have changed again since: it isn't kept.
	if time.Since(fi.ModTime()) > time.Second {
		dirs.Store(key, dirEntries{fi.ModTime(), slices.Clone(entries)})
	}
	return entries, nil
}

// DirReads is how many times ReadDir has read a directory from disk.
func DirReads() int64 { return dirReads.Load() }

// WalkDir is filepath.WalkDir reading directories with ReadDir.
func WalkDir(root string, fn fs.WalkDirFunc) error {
	info, err := os.Lstat(root)
	if err != nil {
		err = fn(root, nil, err)
	} else {
		err = walkDir(root, fs.FileInfoToDirEntry(info), fn)
	}
	if err == filepath.SkipDir || err == filepath.SkipAll {
		return nil
	}
	return err
}

func walkDir(path string, d fs.DirEntry, fn fs.WalkDirFunc) error {
	if err := fn(path, d, nil); err != nil || !d.IsDir() {
		if err == filepath.SkipDir && d.IsDir() {
			err = nil
		}
		return err
	}
	entries, err := ReadDir(path)
	if err != nil {
		if err = fn(path, d, err); err != nil {
			if err == filepath.SkipDir && d.IsDir() {
				err = nil
			}
			return err
		}
	}
	for _, e := range entries {
		if err := walkDir(filepath.Join(path, e.Name()), e, fn); err != nil {
			if err == filepath.SkipDir {
				break
			}
			return err
		}
	}
	return nil
}

// hasGoMod reports whether dir holds a go.mod: the root of a nested module,
// which a walk of the module leaves alone.
func hasGoMod(dir string) bool {
	entries, err := ReadDir(dir)
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

// HasGoMod is hasGoMod for the vuka command.
func HasGoMod(dir string) bool { return hasGoMod(dir) }

type goModEntry struct {
	mod  time.Time
	size int64
	data []byte
}

var goMods sync.Map // dir → goModEntry

// readGoMod is the go.mod in dir, read again only when it changed.
func readGoMod(dir string) ([]byte, error) {
	file := filepath.Join(dir, "go.mod")
	fi, err := os.Stat(file)
	if err != nil {
		return nil, err
	}
	if c, ok := goMods.Load(dir); ok {
		if c := c.(goModEntry); c.mod.Equal(fi.ModTime()) && c.size == fi.Size() {
			return c.data, nil
		}
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	if time.Since(fi.ModTime()) > time.Second {
		goMods.Store(dir, goModEntry{fi.ModTime(), fi.Size(), data})
	}
	return data, nil
}

type sourceEntry struct {
	mod  time.Time
	size int64
	data []byte
}

var sources sync.Map // path → sourceEntry

// ReadSource is os.ReadFile for the module's sources, which a build reads
// from many places: a file is read again only when its size or
// modification time changed.
func ReadSource(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		sources.Delete(path)
		return nil, err
	}
	if c, ok := sources.Load(path); ok {
		if c := c.(sourceEntry); c.mod.Equal(fi.ModTime()) && c.size == fi.Size() {
			return slices.Clone(c.data), nil
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if time.Since(fi.ModTime()) > time.Second && int64(len(data)) == fi.Size() {
		sources.Store(path, sourceEntry{fi.ModTime(), fi.Size(), slices.Clone(data)})
	}
	return data, nil
}
