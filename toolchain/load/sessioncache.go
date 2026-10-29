package load

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"os"
	"path/filepath"
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
// command sets it (its version and its executable). A cache of another
// version is never read.
var CompilerVersion = "dev"

// cacheVersion is the format of the entries.
const cacheVersion = "1"

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

func entryName(kind, path string) string {
	h := sha256.Sum256([]byte(path))
	return filepath.Join(kind, hex.EncodeToString(h[:16]))
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
	VariantMethods []transpile.VariantMethod
}

func (s *Session) loadOut(path string) *pkgOut {
	dir := s.diskDir()
	if dir == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dir, entryName("out", path)))
	if err != nil {
		return nil
	}
	var d diskOut
	if gob.NewDecoder(bytes.NewReader(data)).Decode(&d) != nil || d.Path != path {
		return nil
	}
	o := &pkgOut{key: d.Key, stats: d.Stats}
	for _, g := range d.Gens {
		o.gens = append(o.gens, Generated{Target: g.Target, Source: g.Source, Src: g.Src, From: g.From, Map: g.Map,
			Body: g.Body, Declares: g.Declares, Files: g.Files, Names: g.Names, Vars: g.Vars, Available: g.Avail, Warnings: g.Warnings, Unwrapped: g.Unwrapped, Implicit: g.Implicit, VariantMethods: g.VariantMethods})
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
			Body: g.Body, Declares: g.Declares, Files: g.Files, Names: g.Names, Vars: g.Vars, Avail: g.Available, Warnings: g.Warnings, Unwrapped: g.Unwrapped, Implicit: g.Implicit, VariantMethods: g.VariantMethods})
	}
	var buf bytes.Buffer
	if gob.NewEncoder(&buf).Encode(d) != nil {
		return
	}
	writeEntry(filepath.Join(dir, entryName("out", path)), buf.Bytes())
}

// loadAPI is the API a package whose files and imports make key has, as a
// run checked it before; "" when the cache has none.
func (s *Session) loadAPI(path, key string) string {
	dir := s.diskDir()
	if dir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, entryName("api", path)))
	if err != nil {
		return ""
	}
	f := strings.Fields(string(data))
	if len(f) != 3 || f[0] != path || f[1] != key {
		return ""
	}
	return f[2]
}

func (s *Session) saveAPI(path, key, api string) {
	if dir := s.diskDir(); dir != "" && !strings.ContainsAny(path, " \n") {
		writeEntry(filepath.Join(dir, entryName("api", path)), []byte(path+" "+key+" "+api+"\n"))
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

// pruneCache removes the cache's directories no run has used for two
// weeks: other versions of the compiler, other go.sums.
func (s *Session) pruneCache() {
	if s.cacheBase == "" || s.pruned {
		return
	}
	s.pruned = true
	dir := s.diskDir()
	os.MkdirAll(dir, 0o755)
	now := time.Now()
	os.Chtimes(dir, now, now)
	entries, _ := os.ReadDir(s.cacheBase)
	for _, e := range entries {
		if fi, err := e.Info(); err == nil && e.IsDir() && now.Sub(fi.ModTime()) > 14*24*time.Hour {
			os.RemoveAll(filepath.Join(s.cacheBase, e.Name()))
		}
	}
}
