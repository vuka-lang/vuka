package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// vuka's cache directory stays bounded: at most once a day a build, in the
// background, removes what nothing has used for a while. Only a cache's
// top-level entries go, and each is rebuilt when next needed, so removing
// one under a running vuka costs that vuka a rebuild, never a failure.
// Errors are ignored for the same reason. run trims itself (reuse.go).

const (
	cacheTmpPrefix = ".tmp-"
	cacheMaxAge    = 14 * 24 * time.Hour
	cacheTmpMaxAge = 24 * time.Hour // a temporary file this old was left by a vuka killed mid-write
	cacheInUse     = time.Hour      // nothing used this recently goes, whatever the limits say
	cacheTrimEvery = 24 * time.Hour
	cacheTrimStamp = ".last-trim"
)

// cacheLimit bounds one subdirectory of the cache.
type cacheLimit struct {
	dir      string
	maxAge   time.Duration
	maxItems int   // 0: any number
	maxBytes int64 // 0: any size
	keep     string
}

// cacheLimits are the caches' limits. orm's binaries are large and few are
// wanted; a replace'd orm under edit makes one per edit.
func cacheLimits() []cacheLimit {
	return []cacheLimit{
		{dir: "orm", maxAge: cacheMaxAge, maxItems: 8},
		{dir: "exports", maxAge: cacheMaxAge},
		{dir: "islands", maxAge: cacheMaxAge},
		{dir: "transpile", maxAge: cacheMaxAge, maxBytes: 2 << 30},
		{dir: "vukavet", maxAge: cacheMaxAge, keep: version},
	}
}

// markCacheUsed tells the trim that an entry is in use.
func markCacheUsed(path string) {
	now := time.Now()
	os.Chtimes(path, now, now)
}

// trimCaches trims the cache directory when a day has passed since the last
// trim. Two vukas trimming at once just find less to remove.
func trimCaches(now time.Time) {
	base, ok := load.CacheDir()
	if !ok {
		return
	}
	stamp := filepath.Join(base, cacheTrimStamp)
	if fi, err := os.Stat(stamp); err == nil && now.Sub(fi.ModTime()) < cacheTrimEvery {
		return
	}
	if _, err := os.Stat(base); err != nil {
		return
	}
	if os.WriteFile(stamp, nil, 0o644) != nil {
		return
	}
	os.Chtimes(stamp, now, now)
	for _, l := range cacheLimits() {
		trimCache(filepath.Join(base, l.dir), l, now)
	}
}

type cacheEntry struct {
	path string
	used time.Time
}

func trimCache(dir string, l cacheLimit, now time.Time) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var entries []cacheEntry
	for _, de := range des {
		fi, err := de.Info()
		if err != nil {
			continue
		}
		e := cacheEntry{filepath.Join(dir, de.Name()), fi.ModTime()}
		switch {
		case strings.HasPrefix(de.Name(), cacheTmpPrefix) || strings.HasSuffix(de.Name(), ".tmp"):
			if now.Sub(e.used) > cacheTmpMaxAge {
				os.RemoveAll(e.path)
			}
		case de.Name() == l.keep:
		case l.maxAge > 0 && now.Sub(e.used) > l.maxAge:
			removeCacheEntry(e, now)
		default:
			entries = append(entries, e)
		}
	}
	// The ones used longest ago first.
	slices.SortFunc(entries, func(a, b cacheEntry) int { return a.used.Compare(b.used) })
	if l.maxItems > 0 {
		for len(entries) > l.maxItems && removeCacheEntry(entries[0], now) {
			entries = entries[1:]
		}
	}
	if l.maxBytes > 0 {
		sizes := make([]int64, len(entries))
		var total int64
		for i, e := range entries {
			sizes[i] = cacheEntrySize(e.path)
			total += sizes[i]
		}
		for i := 0; total > l.maxBytes && i < len(entries) && removeCacheEntry(entries[i], now); i++ {
			total -= sizes[i]
		}
	}
}

// removeCacheEntry removes e unless it is in use: its time is read again,
// another vuka may have just marked it. false when it stays; entries are
// tried oldest first, so the ones after it are in use too.
func removeCacheEntry(e cacheEntry, now time.Time) bool {
	fi, err := os.Stat(e.path)
	if err != nil {
		return true // gone already
	}
	if now.Sub(fi.ModTime()) < cacheInUse {
		return false
	}
	os.RemoveAll(e.path)
	return true
}

func cacheEntrySize(path string) int64 {
	var n int64
	filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}
