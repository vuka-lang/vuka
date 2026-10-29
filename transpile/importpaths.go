package transpile

import (
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ImportScope is where a file's package names can come from: the imports
// of its package's other files, and the packages its module can import.
type ImportScope struct {
	// ModuleRoot and ModulePath are the module's directory and path; empty
	// when the file is in none.
	ModuleRoot, ModulePath string
	// Others are the sources of the package's other files.
	Others [][]byte
	// Imports are other files' imports, as [name, path], when their
	// sources aren't at hand.
	Imports [][2]string
}

// toolchainPackages are the import paths of the packages of the vuka
// toolchain set, by the name they go by, each with the module that holds
// it.
var toolchainPackages = map[string][2]string{
	"web":     {"github.com/vuka-lang/web/v2", "github.com/vuka-lang/web/v2"},
	"graphql": {"github.com/vuka-lang/web/v2/graphql", "github.com/vuka-lang/web/v2"},
	"auth":    {"github.com/vuka-lang/web/v2/auth", "github.com/vuka-lang/web/v2"},
	"mail":    {"github.com/vuka-lang/web/v2/mail", "github.com/vuka-lang/web/v2"},
	"cron":    {"github.com/vuka-lang/web/v2/cron", "github.com/vuka-lang/web/v2"},
	"ui":      {"github.com/vuka-lang/ui/v2", "github.com/vuka-lang/ui/v2"},
	"forms":   {"github.com/vuka-lang/ui/v2/forms", "github.com/vuka-lang/ui/v2"},
	"orm":     {"github.com/vuka-lang/orm/v2", "github.com/vuka-lang/orm/v2"},
	"vuka":    {"github.com/vuka-lang/vuka/v2", "github.com/vuka-lang/vuka/v2"},
}

// ImportFor is the one package named name a file can mean, "" when it
// can mean none or several (Candidates).
func (s ImportScope) ImportFor(name string) string {
	if c := s.Candidates(name); len(c) == 1 {
		return c[0]
	}
	return ""
}

// Candidates are the import paths of the packages named name a file can
// mean, sorted. The package's other files decide when they import one by
// that name; else the standard library's, the toolchain's packages of a
// module go.mod requires and the module's own packages; else those of the
// modules go.mod requires directly.
func (s ImportScope) Candidates(name string) []string {
	if name == "" || name == "_" || name == "." {
		return nil
	}
	var found []string
	addTo := func(p string) {
		if !slices.Contains(found, p) {
			found = append(found, p)
		}
	}
	imports := s.Imports
	for _, src := range s.Others {
		imports = append(imports, importsOf(src)...)
	}
	for _, imp := range imports {
		if imp[0] == name {
			addTo(imp[1])
		}
	}
	if len(found) > 0 {
		slices.Sort(found)
		return found
	}
	for _, p := range stdPackages()[name] {
		addTo(p)
	}
	mod := s.goMod()
	if mod != nil && s.ModulePath == "" {
		s.ModulePath = mod.path
	}
	if k, ok := toolchainPackages[name]; ok && (mod == nil || mod.requires(k[1])) {
		addTo(k[0])
	}
	if s.ModuleRoot != "" {
		for _, p := range modulePackages(s.ModuleRoot, s.ModulePath, true)[name] {
			addTo(p)
		}
	}
	if len(found) == 0 && mod != nil {
		for _, r := range mod.direct {
			for _, p := range modulePackages(r.dir, r.path, false)[name] {
				addTo(p)
			}
		}
	}
	slices.Sort(found)
	return found
}

// IsStd reports whether path is a package of the standard library's.
func IsStd(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

// PackageName is the name the package at path declares, when the scope
// can tell: a standard library package, or one of the module's or of a
// module it requires directly; "" when it can't.
func (s ImportScope) PackageName(path string) string {
	if IsStd(path) {
		if slices.Contains(stdPackages()[ImportName(path)], path) {
			return ImportName(path)
		}
		return ""
	}
	dir := ""
	if s.ModulePath != "" && (path == s.ModulePath || strings.HasPrefix(path, s.ModulePath+"/")) {
		dir = filepath.Join(s.ModuleRoot, filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(path, s.ModulePath), "/")))
		if filepath.Base(dir) == "urls" && filepath.Base(filepath.Dir(dir)) == "pages" {
			return "urls"
		}
	} else if mod := s.goMod(); mod != nil {
		for _, r := range mod.direct {
			if path == r.path || strings.HasPrefix(path, r.path+"/") {
				dir = filepath.Join(r.dir, filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(path, r.path), "/")))
				break
			}
		}
	}
	if dir == "" || !hasGoFiles(dir) {
		return ""
	}
	return dirPackage(dir)
}

// importsOf are a file's imports as [name, path] pairs, the name the one
// they go by: a Vuka file's, a template file's or a Go file's.
func importsOf(src []byte) [][2]string {
	af, _ := parser.ParseFile(token.NewFileSet(), "", HeaderView(src), parser.ImportsOnly)
	if af == nil {
		return nil
	}
	var out [][2]string
	for _, imp := range af.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		name := ImportName(p)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		out = append(out, [2]string{name, p})
	}
	return out
}

var (
	stdOnce sync.Once
	stdPkgs map[string][]string
)

// stdPackages are the standard library's importable packages by name.
func stdPackages() map[string][]string {
	stdOnce.Do(func() {
		stdPkgs = map[string][]string{}
		root := filepath.Join(build.Default.GOROOT, "src")
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			rel = filepath.ToSlash(rel)
			if rel == "." {
				return nil
			}
			name := d.Name()
			if name == "testdata" || name == "internal" || name == "vendor" || name == "cmd" && rel == "cmd" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return filepath.SkipDir
			}
			if hasGoFiles(p) {
				stdPkgs[ImportName(rel)] = append(stdPkgs[ImportName(rel)], rel)
			}
			return nil
		})
	})
	return stdPkgs
}

func hasGoFiles(dir string) bool {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && (strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") || strings.HasSuffix(n, ".vuka") || strings.HasSuffix(n, ".vuka.html")) {
			return true
		}
	}
	return false
}

// modCache holds the packages of each module directory read, by name: a
// required module's for good, the module's own for a while, as they change
// while the user writes.
var modCache sync.Map // dir → *modPkgs

type modPkgs struct {
	at   time.Time
	pkgs map[string][]string
}

// modTTL is how long the packages of a module read stay as they were.
var modTTL = 5 * time.Second

// modulePackages are the packages of the module at dir (path), by the
// name they go by: package main left out, and internal ones unless the
// module is the file's own.
func modulePackages(dir, path string, own bool) map[string][]string {
	if v, ok := modCache.Load(dir); ok && (!own || time.Since(v.(*modPkgs).at) < modTTL) {
		return v.(*modPkgs).pkgs
	}
	pkgs := map[string][]string{}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		name := d.Name()
		if p != dir {
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" || name == "node_modules" || name == "vendor" || !own && name == "internal" {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
				return filepath.SkipDir // another module
			}
		}
		if p != dir && !importableElem.MatchString(name) {
			return filepath.SkipDir // a folder route's ([id], (group)): no import path
		}
		rel, _ := filepath.Rel(dir, p)
		imp := path
		if rel != "." {
			imp += "/" + filepath.ToSlash(rel)
		}
		if own && name == "pages" && !slices.Contains(pkgs["urls"], imp+"/urls") {
			pkgs["urls"] = append(pkgs["urls"], imp+"/urls") // vuka generates it for the folder routes
		}
		if !hasGoFiles(p) {
			return nil
		}
		pkg := dirPackage(p)
		if pkg == "main" || pkg == "" {
			return nil
		}
		pkgs[pkg] = append(pkgs[pkg], imp)
		return nil
	})
	modCache.Store(dir, &modPkgs{at: time.Now(), pkgs: pkgs})
	return pkgs
}

var importableElem = regexp.MustCompile(`^[A-Za-z0-9_.~+-]+$`)

var packageLine = regexp.MustCompile(`(?m)^package[ \t]+(\w+)`)

// dirPackage is the name of the package in dir: its files' package
// clause, else the directory's name as an import path's last element.
func dirPackage(dir string) string {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || strings.HasSuffix(n, "_test.go") || !strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, ".vuka") && !strings.HasSuffix(n, ".vuka.html") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		if m := packageLine.FindSubmatch(HeaderView(b)); m != nil {
			return strings.TrimSuffix(string(m[1]), "_test")
		}
	}
	return ImportName(filepath.ToSlash(dir))
}

// goModFile is what Candidates reads of a go.mod.
type goModFile struct {
	path   string // its module's
	text   string
	direct []modDir // the modules it requires directly, where they are
}

type modDir struct{ path, dir string }

func (m *goModFile) requires(mod string) bool {
	return regexp.MustCompile(`(?m)^\s*(?:module\s+|require\s+)?` + regexp.QuoteMeta(mod) + `(?:\s+v|\s*$)`).MatchString(m.text)
}

// goMod reads the module's go.mod; nil when there is none.
func (s ImportScope) goMod() *goModFile {
	if s.ModuleRoot == "" {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(s.ModuleRoot, "go.mod"))
	if err != nil {
		return nil
	}
	m := &goModFile{text: string(b)}
	if f := modLines(m.text, "module"); len(f) > 0 && len(f[0]) > 0 {
		m.path = strings.Trim(f[0][0], `"`)
	}
	replaced := map[string]string{}
	for _, f := range modLines(m.text, "replace") {
		if i := slices.Index(f, "=>"); i > 0 && i+1 < len(f) {
			if to := f[i+1]; strings.HasPrefix(to, ".") || filepath.IsAbs(to) {
				if !filepath.IsAbs(to) {
					to = filepath.Join(s.ModuleRoot, to)
				}
				replaced[f[0]] = to
			}
		}
	}
	cache := modCacheDir()
	for _, f := range modLines(m.text, "require") {
		if len(f) < 2 || slices.Contains(f, "indirect") {
			continue
		}
		dir := replaced[f[0]]
		if dir == "" && cache != "" {
			if esc, ok := escapeModPath(f[0]); ok {
				dir = filepath.Join(cache, esc+"@"+f[1])
			}
		}
		if dir != "" {
			m.direct = append(m.direct, modDir{f[0], dir})
		}
	}
	return m
}

// modLines are the fields of a go.mod's directive lines of kind (require,
// replace), in a block or not; a comment's words follow its //.
func modLines(text, kind string) [][]string {
	var out [][]string
	in := false
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) == 0:
		case in && f[0] == ")":
			in = false
		case in:
			out = append(out, f)
		case f[0] == kind && len(f) > 1 && f[1] == "(":
			in = true
		case f[0] == kind:
			out = append(out, f[1:])
		}
	}
	return out
}

// modCacheDir is the module cache's directory.
func modCacheDir() string {
	if d := os.Getenv("GOMODCACHE"); d != "" {
		return d
	}
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = build.Default.GOPATH
	}
	if gopath == "" {
		return ""
	}
	return filepath.Join(filepath.SplitList(gopath)[0], "pkg", "mod")
}

// escapeModPath is a module path as the module cache writes it: an upper
// case letter as ! and its lower case.
func escapeModPath(path string) (string, bool) {
	var b strings.Builder
	for _, r := range path {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteByte('!')
			b.WriteRune(r + 'a' - 'A')
		case r > 0x7f:
			return "", false
		default:
			b.WriteRune(r)
		}
	}
	return filepath.FromSlash(b.String()), true
}

// ImportablePackage is a package a file can import: the name its files declare,
// its import path and its directory.
type ImportablePackage struct {
	Name, Path, Dir string
	Own             bool // the file's module's
}

// Packages are the packages of the file's module and those of the modules
// go.mod requires directly whose path is one of mods, sorted by path.
func (s ImportScope) Packages(mods ...string) []ImportablePackage {
	var out []ImportablePackage
	add := func(dir, path string, own bool) {
		for name, paths := range modulePackages(dir, path, own) {
			for _, p := range paths {
				d := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(p, path), "/")))
				if hasGoFiles(d) {
					out = append(out, ImportablePackage{Name: name, Path: p, Dir: d, Own: own})
				}
			}
		}
	}
	mod := s.goMod()
	if s.ModuleRoot != "" {
		path := s.ModulePath
		if path == "" && mod != nil {
			path = mod.path
		}
		add(s.ModuleRoot, path, true)
	}
	if mod != nil {
		for _, r := range mod.direct {
			if slices.Contains(mods, r.path) {
				add(r.dir, r.path, false)
			}
		}
	}
	slices.SortFunc(out, func(a, b ImportablePackage) int { return strings.Compare(a.Path, b.Path) })
	return out
}
