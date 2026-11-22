package load

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// A session's disk cache: what it transpiled and the API of the module's
// packages it checked, kept between processes, so a vuka build after a
// vuka build transpiles only the packages whose inputs changed — and
// type-checks only what those need.
//
// It lives under the user's cache directory (vuka/transpile/<key>), the
// key covering everything the session's own key does (the go command, the
// build environment, go.mod, go.sum and go.work) and the compiler
// (CompilerVersion). Each entry is keyed by content as the session's memory
// is: a package's generated files by its inputKey, a package's API by its
// files and its module imports' keys. An entry that can't be read is a
// miss; nothing in it is trusted without its key matching.

// CompilerVersion names the transpiler a cache was written by: the vuka
// command sets it (its build, or its executable when the build says nothing
// of its source). A cache of another version is never read.
var CompilerVersion = "dev"

// cacheVersion is the format of the entries.
const cacheVersion = "4"

// CacheDir is the directory vuka keeps its caches in: VUKA_CACHE when it
// names a directory, else vuka in the user's cache directory. ok is false
// when VUKA_CACHE=off, or there is no such directory.
func CacheDir() (dir string, ok bool) {
	switch v := os.Getenv("VUKA_CACHE"); {
	case strings.EqualFold(v, "off"):
		return "", false
	case v != "":
		if abs, err := filepath.Abs(v); err == nil {
			return abs, true
		}
		return "", false
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return "", false
	}
	return filepath.Join(d, "vuka"), true
}

// UseCache makes the session keep its work on disk, in dir (CacheDir's
// transpile when ""). Off when VUKA_CACHE=off.
func (s *Session) UseCache(dir string) {
	base, ok := CacheDir()
	if !ok {
		return
	}
	if dir == "" {
		dir = filepath.Join(base, "transpile")
	}
	s.cacheBase = dir
}

// diskDir is the cache's directory for the session's current key; "" when
// the session has none.
func (s *Session) diskDir() string {
	if s.cacheBase == "" {
		return ""
	}
	h := sha256.Sum256([]byte(cacheVersion + "\n" + CompilerVersion + "\n" + s.key + "\n" + s.root))
	return filepath.Join(s.cacheBase, hex.EncodeToString(h[:12]))
}

// entryFile is the file holding kind's entry for package path and key.
// Each key has its own, keepOuts of them a package: an edit undone, or a
// branch checked out again, finds the output and API it had before, as a
// session's memory does. Otherwise the packages importing one whose API an
// edit changed are transpiled and type-checked again when it is undone.
func entryFile(dir, kind, path, key string) string {
	p, k := sha256.Sum256([]byte(path)), sha256.Sum256([]byte(key))
	return filepath.Join(dir, kind, hex.EncodeToString(p[:16]), hex.EncodeToString(k[:16]))
}

// readEntry reads an entry, marking it used for trimEntries.
func readEntry(file string) ([]byte, error) {
	data, err := os.ReadFile(file)
	if err == nil {
		now := time.Now()
		os.Chtimes(file, now, now)
	}
	return data, err
}

// saveEntry writes an entry, then keeps the keepOuts last used of its
// package's.
func saveEntry(file string, data []byte) {
	writeEntry(file, data)
	trimEntries(filepath.Dir(file), keepOuts)
}

// trimEntries removes the entries of dir but the n used last.
func trimEntries(dir string, n int) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= n {
		return
	}
	type entry struct {
		name string
		used time.Time
	}
	var es []entry
	for _, e := range entries {
		if fi, err := e.Info(); err == nil && fi.Mode().IsRegular() && !strings.HasPrefix(e.Name(), ".tmp-") {
			es = append(es, entry{e.Name(), fi.ModTime()})
		}
	}
	slices.SortFunc(es, func(a, b entry) int { return b.used.Compare(a.used) })
	for _, e := range es[min(n, len(es)):] {
		os.Remove(filepath.Join(dir, e.name))
	}
}

// diskOut is a package's generated files as the cache keeps them.
type diskOut struct {
	Path, Key, Stats string
	Gens             []diskGen
}

type diskGen struct {
	Target, Source string
	Src, From      []byte
	Map            *transpile.SourceMap
	Body           int
	Declares       bool
	Files          []transpile.FileRef
	Names          map[string]string
	Vars, Avail    []transpile.VarRead
	Warnings       transpile.ErrorList
	Unwrapped      []transpile.Unwrap
	Implicit       []transpile.ImplicitCtx
	Lets           []transpile.SlotLet
	VariantMethods []transpile.VariantMethod
	Matches        []transpile.Match
}

// loadOut is what a run generated for package path from the inputs key
// names; nil when the cache has none.
func (s *Session) loadOut(path, key string) *pkgOut {
	dir := s.diskDir()
	if dir == "" {
		return nil
	}
	data, err := readEntry(entryFile(dir, "out", path, key))
	if err != nil {
		return nil
	}
	var d diskOut
	if gob.NewDecoder(bytes.NewReader(data)).Decode(&d) != nil || d.Path != path || d.Key != key {
		return nil
	}
	o := &pkgOut{key: d.Key, stats: d.Stats}
	for _, g := range d.Gens {
		o.gens = append(o.gens, Generated{Target: g.Target, Source: g.Source, Src: g.Src, From: g.From, Map: g.Map,
			Body: g.Body, Declares: g.Declares, Files: g.Files, Names: g.Names, Vars: g.Vars, Available: g.Avail, Warnings: g.Warnings, Unwrapped: g.Unwrapped, Implicit: g.Implicit, Lets: g.Lets, VariantMethods: g.VariantMethods, Matches: g.Matches})
	}
	return o
}

func (s *Session) saveOut(path string, o *pkgOut) {
	dir := s.diskDir()
	if dir == "" {
		return
	}
	d := diskOut{Path: path, Key: o.key, Stats: o.stats}
	for _, g := range o.gens {
		if g.TemplMap != nil {
			return // templ's own source map: the language server's, never cached
		}
		d.Gens = append(d.Gens, diskGen{Target: g.Target, Source: g.Source, Src: g.Src, From: g.From, Map: g.Map,
			Body: g.Body, Declares: g.Declares, Files: g.Files, Names: g.Names, Vars: g.Vars, Avail: g.Available, Warnings: g.Warnings, Unwrapped: g.Unwrapped, Implicit: g.Implicit, Lets: g.Lets, VariantMethods: g.VariantMethods, Matches: g.Matches})
	}
	var buf bytes.Buffer
	if gob.NewEncoder(&buf).Encode(d) != nil {
		return
	}
	saveEntry(entryFile(dir, "out", path, o.key), buf.Bytes())
}

// diskAPI is a package's API as the cache keeps it: checkedPkg's api, core
// and unnamed.
type diskAPI struct {
	Path, Key, API, Core string
	Unnamed              map[string]string
}

// loadAPI is the API a package whose files and imports' APIs make key
// has, as a run checked it before; nil when the cache has none.
func (s *Session) loadAPI(path, key string) *checkedPkg {
	dir := s.diskDir()
	if dir == "" {
		return nil
	}
	data, err := readEntry(entryFile(dir, "api", path, key))
	if err != nil {
		return nil
	}
	var d diskAPI
	if gob.NewDecoder(bytes.NewReader(data)).Decode(&d) != nil || d.Path != path || d.Key != key || d.API == "" {
		return nil
	}
	return &checkedPkg{api: d.API, core: d.Core, unnamed: d.Unnamed}
}

func (s *Session) saveAPI(path, key string, c *checkedPkg) {
	dir := s.diskDir()
	if dir == "" {
		return
	}
	var buf bytes.Buffer
	if gob.NewEncoder(&buf).Encode(diskAPI{path, key, c.api, c.core, c.unnamed}) == nil {
		saveEntry(entryFile(dir, "api", path, key), buf.Bytes())
	}
}

// writeEntry writes an entry whole: a reader sees the old one or the new.
func writeEntry(file string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), ".tmp-")
	if err != nil {
		return
	}
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil && cerr == nil {
		if os.Rename(tmp.Name(), file) == nil {
			return
		}
	}
	os.Remove(tmp.Name())
}

const (
	// pruneAfter is how long a key's directory may go unused before a prune
	// removes it.
	pruneAfter = 7 * 24 * time.Hour
	// pruneEvery is how often a run looks: pruneStamp, a file of the cache
	// directory, holds when it last did.
	pruneEvery = 24 * time.Hour
	pruneStamp = ".pruned"
)

// pruneCache marks the session's directory as used, and removes the
// directories of other keys — other versions of the compiler, other
// go.sums, other modules — that no run has used for a week. It looks at
// most once a day.
func (s *Session) pruneCache() {
	if s.cacheBase == "" || s.pruned {
		return
	}
	s.pruned = true
	dir := s.diskDir()
	now := time.Now()
	os.MkdirAll(dir, 0o755)
	os.Chtimes(dir, now, now)
	pruneKeys(s.cacheBase, dir, now)
}

// pruneKeys removes the directories of base unused since pruneAfter before
// now, except keep, when the last prune was more than pruneEvery before now.
// A directory is used when a session starts on its key (pruneCache).
func pruneKeys(base, keep string, now time.Time) {
	stamp := filepath.Join(base, pruneStamp)
	if fi, err := os.Stat(stamp); err == nil && now.Sub(fi.ModTime()) < pruneEvery {
		return
	}
	if os.WriteFile(stamp, nil, 0o644) != nil {
		return
	}
	os.Chtimes(stamp, now, now)
	entries, _ := os.ReadDir(base)
	for _, e := range entries {
		path := filepath.Join(base, e.Name())
		if !e.IsDir() || path == keep {
			continue
		}
		if fi, err := e.Info(); err == nil && now.Sub(fi.ModTime()) > pruneAfter {
			os.RemoveAll(path)
		}
	}
}
