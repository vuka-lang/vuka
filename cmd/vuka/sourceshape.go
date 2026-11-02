package main

import (
	"bufio"
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
)

// sourceShapes tells a watch build when the steps reading the whole module
// (the orm's generated code, the route table) can keep their last result:
// it hashes the module's Go as an overlay has it, without comments, line
// directives or the fixed HTML of compiled templates. An edit to fixed HTML
// leaves the hash as it was; any other edit to Go, go.mod or go.sum changes
// it. File contents are hashed, never file events, and files seen before
// aren't parsed again. nil outside a watch: every step then runs.
type sourceShapes struct {
	mu     sync.Mutex
	byFile map[[sha256.Size]byte][sha256.Size]byte
}

func newSourceShapes() *sourceShapes {
	return &sourceShapes{byFile: map[[sha256.Size]byte][sha256.Size]byte{}}
}

// shapes is the running watch's.
var shapes *sourceShapes

// localReplace matches a go.mod replacing a module with a directory.
var localReplace = regexp.MustCompile(`(?m)=>\s*"?(?:\.{1,2}/|/|[A-Za-z]:\\)`)

// module is the hash of the Go of the module at root as overlay has it; ""
// when the module can change unseen: a dependency in a directory or a
// workspace, or a file that can't be read.
func (s *sourceShapes) module(root, overlay string, tests bool) string {
	if s == nil {
		return ""
	}
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil || localReplace.Match(mod) || inWorkspace(root) {
		return ""
	}
	sum, _ := os.ReadFile(filepath.Join(root, "go.sum"))
	ov, err := readOverlay(overlay)
	if err != nil {
		return ""
	}
	files := map[string]string{} // path → where its content is
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "vendor" || name == "node_modules" || name == "testdata" || fileExists(filepath.Join(path, "go.mod"))) {
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
		return ""
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
	paths := make([]string, 0, len(files))
	for p := range files {
		if tests || !strings.HasSuffix(p, "_test.go") {
			paths = append(paths, p)
		}
	}
	slices.Sort(paths)
	h := sha256.New()
	fmt.Fprintf(h, "go.mod %x\ngo.sum %x\n", sha256.Sum256(mod), sha256.Sum256(sum))
	for _, p := range paths {
		src, err := os.ReadFile(files[p])
		if err != nil {
			return ""
		}
		fmt.Fprintf(h, "%s %x\n", p, s.file(src))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func (s *sourceShapes) file(src []byte) [sha256.Size]byte {
	key := sha256.Sum256(src)
	s.mu.Lock()
	shape, ok := s.byFile[key]
	s.mu.Unlock()
	if !ok {
		shape = fileShape(src)
		s.mu.Lock()
		s.byFile[key] = shape
		s.mu.Unlock()
	}
	return shape
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
// frame's fixed HTML. A file that doesn't parse hashes as it is.
func fileShape(src []byte) [sha256.Size]byte {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if err != nil {
		return sha256.Sum256(src)
	}
	skip := frameStatics(fset, f)
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
	slices.SortFunc(out, func(a, b [2]int) int { return a[0] - b[0] })
	merged := out[:0]
	for _, r := range out {
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
