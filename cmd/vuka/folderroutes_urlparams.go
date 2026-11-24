package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"go/ast"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/vuka-lang/vuka/v2/toolchain/load"

	"github.com/vuka-lang/vuka/v2/diag"
)

// urlParams checks the components a page's markup renders: a field of one
// marked with a bare @ui.URL binds the route's path parameter of its name,
// so the page's path must have it (E0340).
func (c *routeChecker) urlParams() {
	bare := map[[2]string][]string{}
	var deps []string
	for _, rt := range c.res.routes {
		if rt.facts != nil {
			for _, lit := range rt.facts.Renders {
				deps = append(deps, lit.Pkg)
			}
		}
	}
	c.t.resolveDeps(deps)
	for _, rt := range c.res.routes {
		if rt.typ == "" || rt.Kind != "live" && rt.Kind != "page" || rt.dir == nil || rt.facts == nil {
			continue
		}
		params := pathParams(rt.Path)
		for _, lit := range rt.facts.Renders {
			key := [2]string{lit.Pkg, lit.Name}
			fields, seen := bare[key]
			if !seen {
				fields = c.t.bareURLFields(lit.Pkg, lit.Name)
				bare[key] = fields
			}
			for _, name := range fields {
				if matchFold(params, name) != "" {
					continue
				}
				d := diag.New("E0340", "%s.%s binds the path parameter {%s} (a bare @ui.URL), and %s's path %s has none", lit.Name, name, name, rt.typ, rt.Path)
				if s := closest(name, params); s != "" {
					d = d.WithHelp("did you mean {%s}? name the field %s, or give @ui.URL the parameter: @ui.URL(\"%s\") reads the query", s, s, name)
				} else if len(params) > 0 {
					d = d.WithHelp("the path's parameters are {%s}; or read the query: @ui.URL(\"%s\")", strings.Join(params, "}, {"), name)
				} else {
					d = d.WithHelp("render it under a [%s] folder, or read the query: @ui.URL(\"%s\")", name, name)
				}
				c.reportAt(d, lit.Pos)
			}
		}
	}
}

func recvTypeName(x ast.Expr) string {
	if s, ok := x.(*ast.StarExpr); ok {
		x = s.X
	}
	if ix, ok := x.(*ast.IndexExpr); ok {
		x = ix.X
	}
	if id, ok := x.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// pathParams are the names of a route path's {name} segments.
func pathParams(path string) []string {
	var out []string
	for _, seg := range strings.Split(path, "/") {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			out = append(out, strings.TrimSuffix(seg[1:len(seg)-1], "..."))
		}
	}
	return out
}

func matchFold(names []string, name string) string {
	for _, n := range names {
		if strings.EqualFold(n, name) {
			return n
		}
	}
	return ""
}

// bareURLFields are the fields of the struct path.name that its Vuka
// source marks with a bare @ui.URL.
func (t *routeTree) bareURLFields(path, name string) []string {
	var out []string
	for _, a := range t.attrs.structURLAttrs(t.pkgDir(path), name) {
		if a.bare {
			out = append(out, a.field)
		}
	}
	return out
}

// pkgDir is the directory of the package path: the module's, or one of its
// dependencies' (none for the standard library, which has no Vuka source).
func (t *routeTree) pkgDir(path string) string {
	for _, d := range t.dirs {
		if d.importPath == path {
			return d.dir
		}
	}
	switch {
	case path == t.modPath:
		return t.root
	case strings.HasPrefix(path, t.modPath+"/"):
		return filepath.Join(t.root, filepath.FromSlash(strings.TrimPrefix(path, t.modPath+"/")))
	}
	first, _, _ := strings.Cut(path, "/")
	if !strings.Contains(first, ".") {
		return ""
	}
	t.resolveDeps([]string{path})
	t.depMu.Lock()
	defer t.depMu.Unlock()
	return t.depDirs[path]
}

// resolveDeps finds the directories of the dependencies paths names at once.
func (t *routeTree) resolveDeps(paths []string) {
	t.depMu.Lock()
	defer t.depMu.Unlock()
	var ask []string
	for _, p := range paths {
		first, _, _ := strings.Cut(p, "/")
		if _, ok := t.depDirs[p]; !ok && strings.Contains(first, ".") && p != t.modPath && !strings.HasPrefix(p, t.modPath+"/") {
			ask = append(ask, p)
		}
	}
	if len(ask) == 0 {
		return
	}
	if t.depDirs == nil {
		t.depDirs = map[string]string{}
	}
	maps.Copy(t.depDirs, load.PackageDirs(t.root, ask))
}

// urlAttrMemo is the @ui.URL attributes of the structs of the directories a
// build's checks read, each directory read once, and kept on disk while its
// Vuka files are unchanged.
type urlAttrMemo struct {
	root  string
	mu    sync.Mutex
	dirs  map[string]map[string][]urlAttr // dir → struct → its attributes
	disk  map[string]urlAttrDir
	read  bool
	dirty bool
}

type urlAttrDir struct {
	Stamp   string
	Structs map[string][]urlAttrRec
}

type urlAttrRec struct {
	Start, End      int
	Field, Key, Def string
	Bare            bool
}

// structURLAttrs are the @ui.URL attributes on the fields of the struct
// typeName, read from the .vuka and .vuka.html files of dir.
func (m *urlAttrMemo) structURLAttrs(dir, typeName string) []urlAttr {
	if dir == "" {
		return nil
	}
	if m == nil {
		return dirURLAttrs(dir)[typeName]
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	structs, ok := m.dirs[dir]
	if !ok {
		structs = m.load(dir)
		if m.dirs == nil {
			m.dirs = map[string]map[string][]urlAttr{}
		}
		m.dirs[dir] = structs
	}
	return structs[typeName]
}

// load is dir's structs from the disk's copy when its Vuka files are those
// it was read from, else read again.
func (m *urlAttrMemo) load(dir string) map[string][]urlAttr {
	if !m.read {
		m.read = true
		if f := m.file(); f != "" {
			if data, err := os.ReadFile(f); err == nil {
				gob.NewDecoder(bytes.NewReader(data)).Decode(&m.disk)
			}
		}
	}
	stamp := vukaStamp(dir)
	if e, ok := m.disk[dir]; ok && stamp != "" && e.Stamp == stamp {
		out := make(map[string][]urlAttr, len(e.Structs))
		for name, recs := range e.Structs {
			for _, r := range recs {
				out[name] = append(out[name], urlAttr{start: r.Start, end: r.End, field: r.Field, key: r.Key, def: r.Def, bare: r.Bare})
			}
		}
		return out
	}
	out := dirURLAttrs(dir)
	if stamp != "" {
		e := urlAttrDir{Stamp: stamp, Structs: map[string][]urlAttrRec{}}
		for name, as := range out {
			for _, a := range as {
				e.Structs[name] = append(e.Structs[name], urlAttrRec{a.start, a.end, a.field, a.key, a.def, a.bare})
			}
		}
		if m.disk == nil {
			m.disk = map[string]urlAttrDir{}
		}
		m.disk[dir], m.dirty = e, true
	}
	return out
}

func (m *urlAttrMemo) file() string {
	d, ok := load.CacheDir()
	if !ok || m.root == "" {
		return ""
	}
	h := sha256.Sum256([]byte(compilerVersion() + "\n" + m.root))
	return filepath.Join(d, "urlattrs", hex.EncodeToString(h[:12])+".gob")
}

// save writes the disk's copy when a directory was read again.
func (m *urlAttrMemo) save() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.file()
	if !m.dirty || f == "" {
		return
	}
	var buf bytes.Buffer
	if gob.NewEncoder(&buf).Encode(m.disk) != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(f), 0o755) == nil && os.WriteFile(f+".tmp", buf.Bytes(), 0o644) == nil {
		os.Rename(f+".tmp", f)
	}
	m.dirty = false
}

// vukaStamp names the Vuka files of dir as they are: their names, sizes and
// modification times; "" when one changed too recently to tell.
func vukaStamp(dir string) string {
	entries, err := load.ReadDir(dir)
	if err != nil {
		return ""
	}
	h := sha256.New()
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".vuka") && !strings.HasSuffix(n, ".vuka.html") {
			continue
		}
		fi, err := e.Info()
		if err != nil || time.Since(fi.ModTime()) < time.Second {
			return ""
		}
		fmt.Fprintf(h, "%s %d %d\n", n, fi.Size(), fi.ModTime().UnixNano())
	}
	return hex.EncodeToString(h.Sum(nil))
}

var structOpen = regexp.MustCompile(`\btype\s+(\w+)\s+struct\s*\{`)

// dirURLAttrs are the @ui.URL attributes of each struct the .vuka and
// .vuka.html files of dir declare, the first file declaring it winning.
func dirURLAttrs(dir string) map[string][]urlAttr {
	out := map[string][]urlAttr{}
	entries, _ := load.ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".vuka") && !strings.HasSuffix(n, ".vuka.html") {
			continue
		}
		src, err := load.ReadSource(filepath.Join(dir, n))
		if err != nil || !bytes.Contains(src, []byte("@ui.URL")) {
			continue
		}
		for _, loc := range structOpen.FindAllSubmatchIndex(src, -1) {
			name := string(src[loc[2]:loc[3]])
			if _, ok := out[name]; ok {
				continue
			}
			depth, end := 0, len(src)
			for i := loc[1] - 1; i < len(src); i++ {
				if src[i] == '{' {
					depth++
				} else if src[i] == '}' {
					if depth--; depth == 0 {
						end = i
						break
					}
				}
			}
			out[name] = urlAttrs(src[loc[1]:end])
		}
	}
	return out
}
