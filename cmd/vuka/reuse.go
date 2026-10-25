package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// vuka run -watch keeps the binaries of its last good builds in vuka's
// cache, each under a key hashing everything its build read: the
// transpiled overlay, the module's Go sources (and those of modules a
// replace or go.work points at), go.mod and go.sum, the embedded files, the
// build flags and environment, the Go and the vuka that built it. A start
// whose key matches a kept binary runs that binary at once, and builds all
// the same, in the background: a build that comes out different (an input
// the key missed) is swapped in, with a warning. -fresh, or VUKA_REUSE=off,
// builds before the app starts, as vuka build always does.

const (
	reuseEnv    = "VUKA_REUSE" // off: no reuse of an earlier session's build
	reuseKeep   = 3            // binaries kept per module: a branch switched back and forth hits
	reuseFormat = "1"          // of the key and the entries
	reuseEntry  = "entry.json"
)

// reuseMaxBytes bounds the binaries kept for a module, together.
var reuseMaxBytes int64 = 2 << 30

// reuseCache is a module's kept binaries.
type reuseCache struct {
	dir    string // <cache>/run/<module>
	hashes sync.Map
	// envFile is the go command's environment file (go env -w).
	envFile string
}

// keptBuild is an entry of the cache.
type keptBuild struct {
	Key   string   `json:"key"`
	Sum   string   `json:"sum"`   // binaryID
	Roots []string `json:"roots"` // what the build's packages embed (embedRoots)
	Size  int64    `json:"size"`
	// Overlay hashes what vuka transpiled for it: a kept build found out
	// of date says whether the transpiler's inputs or go build's were
	// missed.
	Overlay string `json:"overlay"`
	dir     string
	used    time.Time
}

// newReuseCache is the cache of the module at root; nil when reuse is off
// (VUKA_REUSE=off, VUKA_CACHE=off, or fresh).
func newReuseCache(root string, fresh bool) *reuseCache {
	if fresh || strings.EqualFold(os.Getenv(reuseEnv), "off") {
		return nil
	}
	base, ok := load.CacheDir()
	if !ok {
		return nil
	}
	h := sha256.Sum256([]byte(root))
	c := &reuseCache{dir: filepath.Join(base, "run", hex.EncodeToString(h[:12]))}
	pruneModules(filepath.Join(base, "run"), c.dir)
	if v := os.Getenv("GOENV"); v != "" {
		c.envFile = v
	} else if d, err := os.UserConfigDir(); err == nil {
		c.envFile = filepath.Join(d, "go", "env")
	}
	c.trim()
	return c
}

// pruneModules removes the kept builds of modules no run has used for two
// weeks, and marks dir used.
func pruneModules(base, dir string) {
	now := time.Now()
	os.Chtimes(dir, now, now)
	des, _ := os.ReadDir(base)
	for _, de := range des {
		if fi, err := de.Info(); err == nil && de.IsDir() && now.Sub(fi.ModTime()) > 14*24*time.Hour {
			os.RemoveAll(filepath.Join(base, de.Name()))
		}
	}
}

// cutFresh reports whether flags ask for -fresh, and returns them without it.
func cutFresh(flags []string) ([]string, bool) {
	fresh := false
	out := flags[:0:0]
	for _, f := range flags {
		switch strings.TrimLeft(f, "-") {
		case "fresh", "fresh=true":
			fresh = true
		case "fresh=false":
		default:
			out = append(out, f)
		}
	}
	return out, fresh
}

// entries are the kept builds, the last used first.
func (c *reuseCache) entries() []*keptBuild {
	des, _ := os.ReadDir(c.dir)
	var out []*keptBuild
	for _, de := range des {
		if !de.IsDir() || strings.HasPrefix(de.Name(), ".") {
			continue
		}
		dir := filepath.Join(c.dir, de.Name())
		fi, err := os.Stat(filepath.Join(dir, reuseEntry))
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, reuseEntry))
		var e keptBuild
		if err != nil || json.Unmarshal(data, &e) != nil || e.Key == "" {
			continue
		}
		e.dir, e.used = dir, fi.ModTime()
		out = append(out, &e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].used.After(out[j].used) })
	return out
}

func (e *keptBuild) bin() string { return exe(filepath.Join(e.dir, "app")) }

// trim keeps the reuseKeep builds used last, together at most
// reuseMaxBytes, and removes the rest, and what a write or a removal that
// stopped left an hour ago.
func (c *reuseCache) trim() {
	des, _ := os.ReadDir(c.dir)
	for _, de := range des {
		// A write that stopped, or an entry half removed (on Windows, its
		// binary running).
		dir := filepath.Join(c.dir, de.Name())
		if !strings.HasPrefix(de.Name(), ".tmp-") && fileExists(filepath.Join(dir, reuseEntry)) {
			continue
		}
		if fi, err := de.Info(); err == nil && time.Since(fi.ModTime()) > time.Hour {
			os.RemoveAll(dir)
		}
	}
	var size int64
	kept := 0
	for _, e := range c.entries() {
		if kept < reuseKeep && size+e.Size <= reuseMaxBytes {
			kept++
			size += e.Size
			continue
		}
		os.RemoveAll(e.dir) // on Windows not while it runs: the next trim
	}
}

// lookup returns the kept build whose key the sources have now, and that
// key.
func (c *reuseCache) lookup(s *supervisor) (*keptBuild, string) {
	keys := map[string]string{}
	for _, e := range c.entries() {
		rk := strings.Join(e.Roots, "\x00")
		k, ok := keys[rk]
		if !ok {
			k, _, _ = c.key(s, e.Roots)
			keys[rk] = k
		}
		if k != "" && k == e.Key {
			if _, err := os.Stat(e.bin()); err != nil {
				continue
			}
			now := time.Now()
			os.Chtimes(filepath.Join(e.dir, reuseEntry), now, now)
			return e, k
		}
	}
	return nil, ""
}

// lastRoots are what the build used last embedded: the guess for the
// next, which embedRoots tells only once go build runs.
func (c *reuseCache) lastRoots() []string {
	if es := c.entries(); len(es) > 0 {
		return es[0].Roots
	}
	return nil
}

// store keeps r's binary under its key, when the sources were as the key
// says while it was built: hashed again now, they still are, with the
// files r embeds; a file r embeds that the key before the build didn't
// cover must be older than the build.
func (c *reuseCache) store(s *supervisor, r *buildResult) {
	if r.key == "" || r.bin == "" {
		return
	}
	roots := r.embeds
	if roots == nil {
		return // go list failed: what the binary embeds is unknown
	}
	key, newest, err := c.key(s, roots)
	if err != nil {
		return
	}
	if slices.Equal(roots, r.keyRoots) {
		if key != r.key {
			return
		}
	} else if !newest.Before(r.started) {
		return
	}
	fi, err := os.Stat(r.bin)
	if err != nil || fi.Size() > reuseMaxBytes {
		return
	}
	name := key[:32]
	final := filepath.Join(c.dir, name)
	if e, err := os.ReadFile(filepath.Join(final, reuseEntry)); err == nil {
		var old keptBuild
		if json.Unmarshal(e, &old) == nil && old.Key == key && old.Sum == r.sum {
			now := time.Now()
			os.Chtimes(filepath.Join(final, reuseEntry), now, now)
			return
		}
	}
	if os.MkdirAll(c.dir, 0o755) != nil {
		return
	}
	tmp, err := os.MkdirTemp(c.dir, ".tmp-")
	if err != nil {
		return
	}
	defer os.RemoveAll(tmp)
	if linkOrCopy(r.bin, exe(filepath.Join(tmp, "app"))) != nil {
		return
	}
	data, _ := json.Marshal(keptBuild{Key: key, Sum: r.sum, Roots: roots, Size: fi.Size(), Overlay: overlayHash(r.overlay)})
	if os.WriteFile(filepath.Join(tmp, reuseEntry), data, 0o644) != nil {
		return
	}
	if removeAll(final) != nil {
		return
	}
	if os.Rename(tmp, final) != nil {
		return
	}
	c.trim()
}

// linkOrCopy makes dst the file src: a hard link (a binary run from it is
// the file the system checked when it last ran), else a copy.
func linkOrCopy(src, dst string) error {
	if os.Link(src, dst) == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// goBuildExt are the extensions of the files go build reads in a
// package's directory (default.pgo too), and those vuka transpiles into Go.
var goBuildExt = map[string]bool{
	".go": true, ".s": true, ".S": true, ".sx": true, ".c": true, ".cc": true, ".cpp": true, ".cxx": true,
	".h": true, ".hh": true, ".hpp": true, ".hxx": true, ".m": true, ".f": true, ".F": true, ".for": true,
	".f90": true, ".syso": true, ".swig": true, ".swigcxx": true, ".pgo": true,
	".vuka": true, ".templ": true,
	// island components a .vuka file imports, and the files their props'
	// types come from (tsprops)
	".vue": true, ".tsx": true, ".jsx": true, ".svelte": true, ".ts": true, ".mts": true, ".js": true, ".mjs": true,
}

// key hashes what a build reads, the files under roots being what it
// embeds: the sources vuka transpiles and go build compiles, as the
// watcher sees them, those of modules the build reads from directories,
// the embedded files (but those stubbed: their names), the build's flags
// and environment, the Go and the vuka; newest is the latest modification
// time of the files it hashed. It reads what the transpiled overlay is
// made from, so a start computes it without transpiling. "" when a file
// can't be read.
func (c *reuseCache) key(s *supervisor, roots []string) (key string, newest time.Time, err error) {
	h := sha256.New()
	fmt.Fprintf(h, "vuka run -watch %s\n%s\n%s/%s\n", reuseFormat, load.CompilerVersion, runtime.GOOS, runtime.GOARCH)
	// The go command, and the Go it runs for the module.
	if goBin, err := exec.LookPath("go"); err == nil {
		if fi, err := os.Stat(goBin); err == nil {
			fmt.Fprintf(h, "go %s %d %d\n", goBin, fi.Size(), fi.ModTime().UnixNano())
		}
	}
	goVersion, err := moduleGoVersion(s.root)
	if err != nil {
		return "", newest, err
	}
	fmt.Fprintf(h, "goversion %s\n", goVersion)
	// The build's environment and flags.
	var env []string
	for _, kv := range s.env {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "GO") || strings.HasPrefix(name, "CGO_") ||
			slices.Contains([]string{"CC", "CXX", "FC", "AR", "PKG_CONFIG"}, name) {
			env = append(env, kv)
		}
	}
	slices.Sort(env)
	for _, kv := range env {
		fmt.Fprintf(h, "env %q\n", kv)
	}
	if c.envFile != "" {
		data, _ := os.ReadFile(c.envFile)
		fmt.Fprintf(h, "goenv %d %x\n", len(data), sha256.Sum256(data))
	}
	fmt.Fprintf(h, "dir %q\nflags %q\npkg %q\nstub %v\n", s.dir, s.flags, s.pkg, s.stub)
	// The embedded files the build stubs: only their names count.
	stubbed := map[string]bool{}
	if s.stub {
		for _, f := range frontendEmbeds(s.root) {
			stubbed[filepath.Clean(f)] = true
		}
	}
	hashFile := func(kind, path string, fi fs.FileInfo) error {
		if stubbed[path] {
			fmt.Fprintf(h, "%s %q stub\n", kind, c.rel(s.root, path))
			return nil
		}
		sum, err := c.fileHash(path, fi)
		if err != nil {
			return err
		}
		if fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
		fmt.Fprintf(h, "%s %q %s\n", kind, c.rel(s.root, path), sum)
		return nil
	}
	// The module's sources, and those of the modules it builds from
	// directories.
	trees, files := c.moduleTrees(s)
	for _, tree := range trees {
		if err := c.hashTree(tree, hashFile); err != nil {
			return "", newest, err
		}
	}
	for _, f := range files {
		fi, err := os.Stat(f)
		if err != nil {
			fmt.Fprintf(h, "file %q none\n", c.rel(s.root, f))
			continue
		}
		if err := hashFile("file", f, fi); err != nil {
			return "", newest, err
		}
	}
	// The embedded files.
	for _, root := range roots {
		fi, err := os.Stat(root)
		if err != nil {
			fmt.Fprintf(h, "embed %q none\n", c.rel(s.root, root))
			continue
		}
		if !fi.IsDir() {
			if err := hashFile("embed", root, fi); err != nil {
				return "", newest, err
			}
			continue
		}
		err = load.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path != root && load.HasGoMod(path) {
					return filepath.SkipDir // another module's, which no pattern reaches
				}
				fmt.Fprintf(h, "embeddir %q\n", c.rel(s.root, path))
				return nil
			}
			fi, err := entryInfo(path, d)
			if err != nil {
				return err
			}
			return hashFile("embed", path, fi)
		})
		if err != nil {
			return "", newest, err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), newest, nil
}

func (c *reuseCache) rel(root, path string) string {
	if r, err := filepath.Rel(root, path); err == nil {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(path)
}

// hashTree hashes a module directory's sources: its go.mod, go.sum and
// vendor/modules.txt, and each file of a type go build or vuka reads but
// tests, in its directories but hidden ones, node_modules, testdata,
// build/ and nested modules'.
func (c *reuseCache) hashTree(tree string, hashFile func(kind, path string, fi fs.FileInfo) error) error {
	return load.WalkDir(tree, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == tree {
				return nil // gone: the overlay or go says so
			}
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != tree && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "testdata" ||
				path == filepath.Join(tree, "build") || load.HasGoMod(path)) {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(name)
		switch {
		case path == filepath.Join(tree, "go.mod") || path == filepath.Join(tree, "go.sum") ||
			path == filepath.Join(tree, "vendor", "modules.txt"):
		case !goBuildExt[ext] || strings.HasPrefix(name, ".") || strings.HasSuffix(name, "_test"+ext):
			if !strings.HasSuffix(name, ".vuka.html") {
				return nil
			}
		}
		fi, err := entryInfo(path, d)
		if err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		return hashFile("src", path, fi)
	})
}

type hashedFile struct {
	mod  time.Time
	size int64
	id   uint64
	sum  string
}

// fileHash is the hash of the file at path, kept while its size,
// modification time and identity stay (as ReadSource keeps a source): a
// file changed within the clock's resolution of being read is read again.
func (c *reuseCache) fileHash(path string, fi fs.FileInfo) (string, error) {
	if v, ok := c.hashes.Load(path); ok {
		if v := v.(hashedFile); v.mod.Equal(fi.ModTime()) && v.size == fi.Size() && v.id == fileID(fi) {
			return v.sum, nil
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hh := sha256.New()
	n, err := io.Copy(hh, f)
	if err != nil {
		return "", err
	}
	sum := hex.EncodeToString(hh.Sum(nil))
	if n == fi.Size() && time.Since(fi.ModTime()) > time.Second {
		c.hashes.Store(path, hashedFile{fi.ModTime(), fi.Size(), fileID(fi), sum})
	}
	return sum, nil
}

// moduleTrees are the directories of the modules the build reads from
// disk: the module, and those a replace directive of its go.mod or of the
// go.work in effect points at, and the go.work's; and the go.work with its
// go.work.sum.
func (c *reuseCache) moduleTrees(s *supervisor) (trees, files []string) {
	trees = []string{s.root}
	mods := []string{filepath.Join(s.root, "go.mod")}
	if work := goWorkFile(s.dir, s.env); work != "" {
		files = []string{work, work + ".sum"}
		uses, reps := parseModFile(work)
		for _, u := range uses {
			trees = append(trees, u)
			mods = append(mods, filepath.Join(u, "go.mod"))
		}
		trees = append(trees, reps...)
	}
	for _, m := range mods {
		_, reps := parseModFile(m)
		trees = append(trees, reps...)
	}
	slices.Sort(trees)
	return slices.Compact(trees), files
}

// goWorkFile is the go.work the go command uses in dir: GOWORK's, or the
// first one up from dir; "" with GOWORK=off.
func goWorkFile(dir string, env []string) string {
	gowork := ""
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "GOWORK="); ok {
			gowork = v
		}
	}
	switch gowork {
	case "off":
		return ""
	case "", "auto":
		for d := dir; ; d = filepath.Dir(d) {
			if fileExists(filepath.Join(d, "go.work")) {
				return filepath.Join(d, "go.work")
			}
			if filepath.Dir(d) == d {
				return ""
			}
		}
	}
	return gowork
}

// parseModFile reads a go.mod's or go.work's use directives and the
// directories its replace directives point at, both made absolute.
func parseModFile(file string) (uses, replaces []string) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, nil
	}
	base := filepath.Dir(file)
	local := func(p string) (string, bool) {
		p = strings.Trim(p, "\"`")
		if !(strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") || strings.HasPrefix(p, `.\`) ||
			strings.HasPrefix(p, `..\`) || filepath.IsAbs(p) || p == "." || p == "..") {
			return "", false
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(base, p)
		}
		return filepath.Clean(p), true
	}
	block := ""
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if block != "" {
			if f[0] == ")" {
				block = ""
				continue
			}
			f = append([]string{block}, f...)
		} else if len(f) == 2 && f[1] == "(" {
			block = f[0]
			continue
		}
		switch f[0] {
		case "use":
			if len(f) >= 2 {
				if p, ok := local(f[1]); ok {
					uses = append(uses, p)
				}
			}
		case "replace":
			if i := slices.Index(f, "=>"); i >= 0 && i+1 < len(f) && len(f) == i+2 {
				if p, ok := local(f[i+1]); ok {
					replaces = append(replaces, p)
				}
			}
		}
	}
	return uses, replaces
}

// overlayHash hashes an overlay's files: what they replace, and with what.
func overlayHash(overlay string) string {
	h := sha256.New()
	if overlay != "" {
		data, err := os.ReadFile(overlay)
		var ov struct{ Replace map[string]string }
		if err != nil || json.Unmarshal(data, &ov) != nil {
			return ""
		}
		for _, from := range slices.Sorted(maps.Keys(ov.Replace)) {
			sum := "-"
			if to := ov.Replace[from]; to != "" {
				b, err := os.ReadFile(to)
				if err != nil {
					return ""
				}
				sum = fmt.Sprintf("%x", sha256.Sum256(b))
			}
			fmt.Fprintf(h, "%q %s\n", from, sum)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// predictCSS is the stylesheet the app is told of (devTailwind's) when it
// can be told without the transpiled sources: Tailwind's output, when the
// module uses Tailwind and its CLI is installed.
func predictCSS(root string) (string, bool) {
	tw, err := tailwindFor(root)
	if err != nil || tw == nil {
		return "", false // the pages' own stylesheets, if they declare any: known once transpiled
	}
	if _, err := tailwindBinary(root); err != nil {
		return "", false
	}
	return filepath.Join(root, filepath.FromSlash(twCacheDir), "app.css"), true
}
