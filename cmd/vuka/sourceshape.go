package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// sourceShapes tells a watch build when the steps reading the whole module
// (the orm's generated code, the route table) can keep their last result:
// it hashes the module's Go as an overlay has it, without comments, line
// directives or the fixed HTML of compiled templates, and, in the files a
// step reads only the declarations of, without function bodies. An edit to
// anything else, go.mod or go.sum, or to a module it builds from a
// directory, changes the hash. File contents are hashed,
// never file events, and files seen before aren't parsed again. nil outside a
// watch: every step then runs.
type sourceShapes struct {
	mu     sync.Mutex
	byFile map[[sha256.Size]byte]fileShapes
	sums   map[string]sumOf  // the local modules' files (localSources), by path
	pinned map[string]string // a build's localSources, by module root (pin)
	snaps  map[string]*load.Snapshot
}

// pin hashes the local modules of the module at root (localSources) for a
// build: its steps' shapes take that hash rather than each walking them
// again. Until the build is done (unpin) they read the module through snap.
// It returns the hash, "" for none.
func (s *sourceShapes) pin(root string, snap *load.Snapshot) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	s.snaps[root] = snap
	s.mu.Unlock()
	local := ""
	if mod, err := snap.ReadFile(filepath.Join(root, "go.mod")); err == nil && (localReplace.Match(mod) || inWorkspace(root)) {
		local = s.localSources(root)
	}
	s.mu.Lock()
	s.pinned[root] = local
	s.mu.Unlock()
	return local
}

// unpin has the steps read the module at root from disk again, once the
// build pin was for is done.
func (s *sourceShapes) unpin(root string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.snaps, root)
	s.mu.Unlock()
}

// snap is what a build reads the module at root through: nil for the disk.
func (s *sourceShapes) snap(root string) *load.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snaps[root]
}

// fileShapes are a file's hashes: of everything; without the function
// bodies a step doesn't read; and, for the orm's step, without the functions
// it doesn't read either.
type fileShapes struct {
	full, decls, models [sha256.Size]byte
	orm                 *ormFileFacts
}

// shapeKind is what of a file a step reads.
type shapeKind int

const (
	shapeFull   shapeKind = iota // everything
	shapeDecls                   // the declarations, and the bodies unreadBodies keeps
	shapeModels                  // shapeDecls without the functions ormUnread names
)

func newSourceShapes() *sourceShapes {
	return &sourceShapes{byFile: map[[sha256.Size]byte]fileShapes{}, sums: map[string]sumOf{}, pinned: map[string]string{}, snaps: map[string]*load.Snapshot{}}
}

// shapes is the running watch's.
var shapes *sourceShapes

// localReplace matches a go.mod replacing a module with a directory.
var localReplace = regexp.MustCompile(`(?m)=>\s*"?(?:\.{1,2}/|/|[A-Za-z]:\\)`)

// module is the hash of the Go of the module at root as overlay has it,
// with the sources of the modules it builds from directories (a replace
// directive's, a workspace's: localSources); "" when a file can't be read.
// kind says what of each of the module's files counts; a nil kind counts
// everything.
func (s *sourceShapes) module(root, overlay string, tests bool, kind func(path string) shapeKind) string {
	if s == nil {
		return ""
	}
	head, paths, files, ok := s.moduleFiles(root, overlay, tests)
	if !ok {
		return ""
	}
	snap := s.snap(root)
	h := sha256.New()
	h.Write([]byte(head))
	for _, p := range paths {
		src, err := snap.ReadFile(files[p])
		if err != nil {
			return ""
		}
		fmt.Fprintf(h, "%s %x\n", p, s.file(src).of(kind, p))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// of is the hash of the file at path kind says counts; a nil kind counts
// everything.
func (f fileShapes) of(kind func(path string) shapeKind, path string) [sha256.Size]byte {
	k := shapeFull
	if kind != nil {
		k = kind(path)
	}
	switch k {
	case shapeFull:
		return f.full
	case shapeDecls:
		return f.decls
	}
	return f.models
}

// moduleFiles is what module hashes: the line naming go.mod, go.sum and
// the local modules, and the module's Go files as overlay has them, in
// order, each with where its content is. Not ok when one can't be read.
func (s *sourceShapes) moduleFiles(root, overlay string, tests bool) (head string, paths []string, files map[string]string, ok bool) {
	snap := s.snap(root)
	mod, err := snap.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", nil, nil, false
	}
	local := ""
	if localReplace.Match(mod) || inWorkspace(root) {
		s.mu.Lock()
		local = s.pinned[root]
		s.mu.Unlock()
		if local == "" {
			local = s.localSources(root)
		}
		if local == "" {
			return "", nil, nil, false
		}
	}
	sum, _ := snap.ReadFile(filepath.Join(root, "go.sum"))
	ov, err := readOverlay(overlay)
	if err != nil {
		return "", nil, nil, false
	}
	files = map[string]string{} // path → where its content is
	err = snap.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "vendor" || name == "node_modules" || name == "testdata" || snap.Exists(filepath.Join(path, "go.mod"))) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			files[path] = path
		}
		return nil
	})
	if err != nil {
		return "", nil, nil, false
	}
	for target, file := range ov {
		switch {
		case !strings.HasSuffix(target, ".go"):
		case file == "":
			delete(files, target)
		default:
			files[target] = file
		}
	}
	for p := range files {
		if tests || !strings.HasSuffix(p, "_test.go") {
			paths = append(paths, p)
		}
	}
	slices.Sort(paths)
	head = fmt.Sprintf("go.mod %x\ngo.sum %x\nlocal %s\n", sha256.Sum256(mod), sha256.Sum256(sum), local)
	return head, paths, files, true
}

func (s *sourceShapes) file(src []byte) fileShapes {
	key := sha256.Sum256(src)
	s.mu.Lock()
	shape, ok := s.byFile[key]
	s.mu.Unlock()
	if !ok {
		shape = shapesOf(src)
		s.mu.Lock()
		s.byFile[key] = shape
		s.mu.Unlock()
	}
	return shape
}

// localSources hashes the sources of the modules the module at root builds
// from directories, which change without its go.mod changing: each file of
// a type go build or vuka reads (but tests) of the directories a replace
// directive or the go.work points at, and the go.work with its go.work.sum.
// A file's hash is kept while its size, modification time and identity
// stay. "" when a file can't be read.
func (s *sourceShapes) localSources(root string) string {
	snap := s.snap(root)
	trees, files := moduleTrees(root, root, os.Environ())
	h := sha256.New()
	hashFile := func(path string, fi fs.FileInfo) error {
		sum, err := s.fileSum(path, fi)
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s %x\n", path, sum)
		return nil
	}
	for _, f := range files {
		fi, err := snap.Stat(f)
		if err != nil {
			fmt.Fprintf(h, "%s none\n", f)
			continue
		}
		if err := hashFile(f, fi); err != nil {
			return ""
		}
	}
	for _, tree := range trees {
		if tree == root {
			continue // the module: module hashes it as the overlay has it
		}
		fmt.Fprintf(h, "tree %s\n", tree)
		err := snap.WalkDir(tree, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if path == tree {
					return nil // gone: go.mod's directive fails the build
				}
				return err
			}
			name := d.Name()
			if d.IsDir() {
				if path != tree && (strings.HasPrefix(name, ".") || privateSkipped(tree, path, name) || name == "node_modules" || name == "testdata" ||
					snap.Exists(filepath.Join(path, "go.mod"))) {
					return filepath.SkipDir
				}
				return nil
			}
			ext := filepath.Ext(name)
			switch {
			case path == filepath.Join(tree, "go.mod") || path == filepath.Join(tree, "go.sum"):
			case !goBuildExt[ext] && !strings.HasSuffix(name, ".vuka.html") || strings.HasPrefix(name, ".") || strings.HasSuffix(name, "_test"+ext):
				return nil
			}
			fi, err := d.Info()
			if err != nil {
				return err
			}
			if !fi.Mode().IsRegular() {
				return nil
			}
			return hashFile(path, fi)
		})
		if err != nil {
			return ""
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// sumOf is a file's hash, kept while its size, modification time and
// identity stay.
type sumOf struct {
	mod  time.Time
	size int64
	id   uint64
	sum  [sha256.Size]byte
}

// fileSum is the hash of the file at path, as fileHash keeps it: one
// changed within a second of being read is read again next time.
func (s *sourceShapes) fileSum(path string, fi fs.FileInfo) ([sha256.Size]byte, error) {
	s.mu.Lock()
	v, ok := s.sums[path]
	s.mu.Unlock()
	if ok && v.mod.Equal(fi.ModTime()) && v.size == fi.Size() && v.id == fileID(fi) {
		return v.sum, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	sum := sha256.Sum256(data)
	if int64(len(data)) == fi.Size() && time.Since(fi.ModTime()) > time.Second {
		s.mu.Lock()
		s.sums[path] = sumOf{fi.ModTime(), fi.Size(), fileID(fi), sum}
		s.mu.Unlock()
	}
	return sum, nil
}

// inWorkspace reports whether a go.work applies to the module at root.
func inWorkspace(root string) bool {
	switch w := os.Getenv("GOWORK"); w {
	case "off":
		return false
	case "":
	default:
		return true
	}
	for dir := root; ; dir = filepath.Dir(dir) {
		if fileExists(filepath.Join(dir, "go.work")) {
			return true
		}
		if dir == filepath.Dir(dir) {
			return false
		}
	}
}

// fileShape hashes src's tokens, leaving out comments and each template
// frame's fixed HTML.
func fileShape(src []byte) [sha256.Size]byte { return shapesOf(src).full }

// shapesOf hashes src every way. A file that doesn't parse hashes as it is.
func shapesOf(src []byte) fileShapes {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if err != nil {
		sum := sha256.Sum256(src)
		return fileShapes{sum, sum, sum, &ormFileFacts{Broken: true}}
	}
	statics := frameStatics(fset, f)
	unread := unreadBodies(fset, f)
	decls := mergeRanges(append(slices.Clone(statics), unread...))
	return fileShapes{
		full:   hashTokens(src, statics),
		decls:  hashTokens(src, decls),
		models: hashTokens(src, mergeRanges(append(decls, ormUnread(fset, f, src)...))),
		orm:    ormFactsOf(f, src),
	}
}

// ormUnread are the byte ranges of the declarations of f the orm's step
// doesn't read, whole: it reads types, methods (a Schema method, a field
// type's Scan and Value), variables, the bodies with an orm.For[T](…), and
// of the package's functions and constants only whether one is named as its
// generated code would be (UserFields, UserColumns). A function whose
// source names For[, Model[ or Base[ counts in full: that makes a package
// one the orm looks at.
func ormUnread(fset *token.FileSet, f *ast.File, src []byte) [][2]int {
	generatedName := func(name string) bool {
		return strings.HasSuffix(name, "Fields") || strings.HasSuffix(name, "Columns")
	}
	var out [][2]int
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv != nil || generatedName(d.Name.Name) {
				continue
			}
		case *ast.GenDecl:
			if d.Tok != token.CONST || slices.ContainsFunc(d.Specs, func(s ast.Spec) bool {
				return slices.ContainsFunc(s.(*ast.ValueSpec).Names, func(n *ast.Ident) bool { return generatedName(n.Name) })
			}) {
				continue
			}
		default:
			continue
		}
		from, to := fset.Position(decl.Pos()).Offset, fset.Position(decl.End()).Offset
		if text := src[from:to]; bytes.Contains(text, []byte("For[")) || bytes.Contains(text, []byte("Model[")) || bytes.Contains(text, []byte("Base[")) {
			continue
		}
		for to < len(src) && (src[to] == ' ' || src[to] == '\t' || src[to] == '\r') {
			to++
		}
		if to < len(src) && src[to] == '\n' {
			to++ // the semicolon the scanner adds after the closing brace
		}
		out = append(out, [2]int{from, to})
	}
	return out
}

// hashTokens hashes src's tokens outside skip's byte ranges, which are in
// order and don't overlap.
func hashTokens(src []byte, skip [][2]int) [sha256.Size]byte {
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(src))
	var sc scanner.Scanner
	sc.Init(file, src, nil, 0)
	h := sha256.New()
	w := bufio.NewWriter(h)
	for {
		pos, tok, lit := sc.Scan()
		if tok == token.EOF {
			break
		}
		off := file.Offset(pos)
		for len(skip) > 0 && off >= skip[0][1] {
			skip = skip[1:]
		}
		if len(skip) > 0 && off >= skip[0][0] {
			continue
		}
		fmt.Fprintf(w, "%d %s\n", tok, lit)
	}
	w.Flush()
	return [sha256.Size]byte(h.Sum(nil))
}

// unreadBodies are the byte ranges of f's function bodies no step reads outside
// the files it names. The orm reads two kinds: a Schema method's, for the
// columns it renames, and any with an orm.For[T](…), which makes T a model.
func unreadBodies(fset *token.FileSet, f *ast.File) [][2]int {
	var out [][2]int
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Recv != nil && fn.Name.Name == "Schema" || callsFor(fn.Body) {
			continue
		}
		out = append(out, [2]int{fset.Position(fn.Body.Pos()).Offset, fset.Position(fn.Body.End()).Offset})
	}
	return out
}

// callsFor reports whether n holds a call of a generic func named For.
func callsFor(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if ix, ok := call.Fun.(*ast.IndexExpr); ok {
				switch f := ix.X.(type) {
				case *ast.SelectorExpr:
					found = found || f.Sel.Name == "For"
				case *ast.Ident:
					found = found || f.Name == "For"
				}
			}
		}
		return !found
	})
	return found
}

// frameStatics are the byte ranges of f holding fixed HTML, in order: the
// markup a template passes to ui.HTML, the fingerprint, shape and builder it
// passes to ui.FT, and the fingerprint and shape of a ui.F outside one. Their
// expressions are the arguments after them.
func frameStatics(fset *token.FileSet, f *ast.File) [][2]int {
	var out [][2]int
	offset := func(p token.Pos) int { return fset.Position(p).Offset }
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		last := -1
		switch frameFunc(call) {
		case "HTML":
			if _, sel := call.Fun.(*ast.SelectorExpr); sel && len(call.Args) >= 1 && isBasicLit(call.Args[0], token.STRING) {
				last = 0
			}
		case "FT":
			if len(call.Args) >= 3 && isFrameBuilder(call.Args[2]) {
				last = 2
			}
		case "F":
			if len(call.Args) >= 2 && isBasicLit(call.Args[0], token.INT) && isBasicLit(call.Args[1], token.STRING) {
				last = 1
			}
		}
		if last >= 0 {
			out = append(out, [2]int{offset(call.Args[0].Pos()), offset(call.Args[last].End())})
		}
		return true
	})
	return mergeRanges(out)
}

// mergeRanges puts byte ranges in order, joining those that overlap.
func mergeRanges(ranges [][2]int) [][2]int {
	slices.SortFunc(ranges, func(a, b [2]int) int { return a[0] - b[0] })
	merged := ranges[:0]
	for _, r := range ranges {
		if n := len(merged); n > 0 && r[0] < merged[n-1][1] {
			merged[n-1][1] = max(merged[n-1][1], r[1])
			continue
		}
		merged = append(merged, r)
	}
	return merged
}

func frameFunc(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return ""
}

func isBasicLit(e ast.Expr, kind token.Token) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && lit.Kind == kind
}

// isFrameBuilder reports whether e is the func(__d []any) a compiled
// template passes to ui.FT.
func isFrameBuilder(e ast.Expr) bool {
	fn, ok := e.(*ast.FuncLit)
	if !ok || fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
		return false
	}
	names := fn.Type.Params.List[0].Names
	return len(names) == 1 && names[0].Name == "__d"
}
