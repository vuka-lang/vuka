package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

// vuka ui add <library>/<name>: a component of a templ component library,
// written into the app's components directory as its own source, from the
// library's Go module. What vuka ui needs to know of a library is a
// libSpec, vuka's own description of where things are in its module: no
// registry format of the library's is read, so its components come from
// its source at a version, the way go get finds them.

// libSpec is a templ component library: where its components, scripts and
// theme are in its Go module.
type libSpec struct {
	Name    string
	Module  string // its Go module's path
	Version string // the version vuka ui add takes
	// Dir is a directory holding the module's source, read in place of
	// Module at Version: a library being written beside the app. Relative
	// to the app's module.
	Dir string
	// Components is the directory of the module holding the component
	// packages, one directory each.
	Components string
	// Scripts are directories of Components holding scripts the others
	// share: written into the app with the first component that has a
	// script, and bundled before the components' own.
	Scripts []string
	// Theme is a stylesheet of the module whose :root, .dark and @theme
	// inline blocks declare the theme's variables; the app gets those its
	// stylesheet doesn't declare.
	Theme string
	// CSS are stylesheets of the module the components need (variants,
	// keyframes, utilities), written beside the app's and imported.
	CSS []string
	// Style is a stylesheet of the module styling the components' own
	// classes; the app gets the rules for the classes its components use.
	Style string
}

// builtinLibraries are the libraries vuka ui knows. vuka.toml's
// [ui.libraries.<name>] adds others, or changes one of these (its version).
var builtinLibraries = map[string]libSpec{
	"shadcn-templ": {
		Module:     "github.com/axadrn/shadcn-templ/v2",
		Version:    "v2.0.0-beta.16",
		Components: "components",
		Scripts:    []string{"baseui", "floatingui"},
		Theme:      "assets/css/globals.css",
		CSS:        []string{"assets/css/tw-animate.css", "assets/css/shadcn-tailwind.css"},
		Style:      "assets/css/styles/style-nova.css",
	},
}

// uiLibConfig is what vuka.toml says of libraries: [ui.sources], where
// each directory of the components directory came from (its name → the
// library@version, or the library alone for one read from a directory),
// and [ui.libraries.<name>].
type uiLibConfig struct {
	sources map[string]string
	libs    map[string]libSpec
}

var libKeys = []string{"module", "version", "dir", "components", "scripts", "theme", "css", "style"}

// readUILib reads [ui.sources] and [ui.libraries.<name>] of the vuka.toml
// at root.
func readUILib(root string) (*uiLibConfig, error) {
	lc := &uiLibConfig{sources: map[string]string{}, libs: map[string]libSpec{}}
	src, err := os.ReadFile(filepath.Join(root, "vuka.toml"))
	if errors.Is(err, os.ErrNotExist) {
		return lc, nil
	} else if err != nil {
		return nil, err
	}
	table := ""
	for n, line := range strings.Split(string(src), "\n") {
		l := strings.TrimSpace(stripComment(line))
		switch {
		case l == "":
			continue
		case strings.HasPrefix(l, "["):
			table = strings.TrimSpace(strings.Trim(l, "[]"))
			if name, ok := strings.CutPrefix(table, "ui.libraries."); ok {
				if !itemName.MatchString(name) {
					return nil, fmt.Errorf("vuka.toml:%d: [%s]: a library's name is in lower case", n+1, table)
				}
				if _, ok := lc.libs[name]; !ok {
					lc.libs[name] = libSpec{Name: name}
				}
			}
			continue
		}
		at := fmt.Sprintf("vuka.toml:%d", n+1)
		key, value, found := strings.Cut(l, "=")
		key, value = strings.Trim(strings.TrimSpace(key), `"`), strings.TrimSpace(value)
		switch {
		case table == "ui.sources":
			s, err := strconv.Unquote(value)
			if !found || err != nil {
				return nil, fmt.Errorf("%s: [ui.sources] has name = \"library@version\" lines", at)
			}
			lc.sources[key] = s
		case strings.HasPrefix(table, "ui.libraries."):
			name := strings.TrimPrefix(table, "ui.libraries.")
			spec := lc.libs[name]
			list := func() ([]string, error) {
				if s, err := strconv.Unquote(value); err == nil {
					return []string{s}, nil
				}
				return tomlStrings(value)
			}
			var err error
			s, qerr := strconv.Unquote(value)
			switch key {
			case "module":
				spec.Module, err = s, qerr
			case "version":
				spec.Version, err = s, qerr
			case "dir":
				spec.Dir, err = s, qerr
			case "components":
				spec.Components, err = s, qerr
			case "theme":
				spec.Theme, err = s, qerr
			case "style":
				spec.Style, err = s, qerr
			case "scripts":
				spec.Scripts, err = list()
			case "css":
				spec.CSS, err = list()
			default:
				return nil, fmt.Errorf("%s: unknown key %s in [%s]%s; its keys are %s", at, key, table, suggest(key, libKeys, ""), strings.Join(libKeys, ", "))
			}
			if !found || err != nil {
				return nil, fmt.Errorf("%s: [%s] has key = \"value\" lines (scripts and css: a list of strings)", at, table)
			}
			lc.libs[name] = spec
		}
	}
	return lc, nil
}

// tomlStrings is a TOML array of strings on one line: ["a", "b"].
func tomlStrings(v string) ([]string, error) {
	inner, ok := strings.CutPrefix(v, "[")
	if inner, ok = strings.CutSuffix(inner, "]"); !ok {
		return nil, errors.New("not a list")
	}
	var out []string
	for _, p := range strings.Split(inner, ",") {
		if p = strings.TrimSpace(p); p == "" {
			continue
		}
		s, err := strconv.Unquote(p)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// library is the library named, vuka's own description of it with what
// vuka.toml changes.
func (lc *uiLibConfig) library(name string) (libSpec, error) {
	spec, builtin := builtinLibraries[name]
	conf, configured := lc.libs[name]
	if !builtin && !configured {
		names := slices.Sorted(mapKeys(builtinLibraries))
		for n := range lc.libs {
			names = append(names, n)
		}
		slices.Sort(names)
		return spec, fmt.Errorf("vuka ui: no library %q%s; the libraries are %s (vuka.toml's [ui.libraries.<name>] adds one)", name, suggest(name, names, ""), strings.Join(names, ", "))
	}
	spec.Name = name
	for _, f := range []struct{ to, from *string }{{&spec.Module, &conf.Module}, {&spec.Version, &conf.Version}, {&spec.Dir, &conf.Dir}, {&spec.Components, &conf.Components}, {&spec.Theme, &conf.Theme}, {&spec.Style, &conf.Style}} {
		if *f.from != "" {
			*f.to = *f.from
		}
	}
	if conf.Scripts != nil {
		spec.Scripts = conf.Scripts
	}
	if conf.CSS != nil {
		spec.CSS = conf.CSS
	}
	if conf.Dir != "" && conf.Module == "" {
		spec.Module = "" // read from the directory's go.mod
	}
	if spec.Components == "" {
		spec.Components = "components"
	}
	switch {
	case spec.Module == "" && spec.Dir == "":
		return spec, fmt.Errorf("vuka.toml: [ui.libraries.%s] names its Go module (module and version) or a directory holding it (dir)", name)
	case spec.Dir == "" && spec.Version == "":
		return spec, fmt.Errorf("vuka.toml: [ui.libraries.%s] names the version of %s vuka ui add takes (version)", name, spec.Module)
	case spec.Version != "" && !semver.IsValid(spec.Version):
		return spec, fmt.Errorf("vuka.toml: [ui.libraries.%s] version %q isn't a module version (vX.Y.Z)", name, spec.Version)
	}
	return spec, nil
}

// libSource is a library's source at a version: its module's directory.
type libSource struct {
	spec    libSpec
	version string // "" for a library read from a directory
	dir     string
	mod     *modfile.File
}

// label is the source's record in [ui.sources]: name@version.
func (s *libSource) label() string {
	if s.version == "" {
		return s.spec.Name
	}
	return s.spec.Name + "@" + s.version
}

// openLibrary finds the source of the library at version ("" for the
// spec's): the module's directory in the module cache, which go mod
// download fills, or the directory the spec names.
func openLibrary(root string, spec libSpec, version string) (*libSource, error) {
	s := &libSource{spec: spec}
	if spec.Dir != "" {
		dir := filepath.FromSlash(spec.Dir)
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, dir)
		}
		s.dir = dir
	} else {
		if version == "" {
			version = spec.Version
		}
		s.version = version
		c := exec.Command("go", "mod", "download", "-json", spec.Module+"@"+version)
		c.Dir = root
		var stderr bytes.Buffer
		c.Stderr = &stderr
		out, _ := c.Output()
		var dl struct{ Dir, Error string }
		if err := json.Unmarshal(out, &dl); err != nil || dl.Error != "" || dl.Dir == "" {
			msg := dl.Error
			if msg == "" {
				msg = strings.TrimSpace(stderr.String())
			}
			return nil, fmt.Errorf("vuka ui: fetching %s@%s: %s", spec.Module, version, msg)
		}
		s.dir = dl.Dir
	}
	gomod := filepath.Join(s.dir, "go.mod")
	b, err := os.ReadFile(gomod)
	if err != nil {
		return nil, fmt.Errorf("vuka ui: the library %s: %v", spec.Name, err)
	}
	if s.mod, err = modfile.ParseLax(gomod, b, nil); err != nil {
		return nil, fmt.Errorf("vuka ui: %v", err)
	}
	if s.mod.Module == nil {
		return nil, fmt.Errorf("vuka ui: %s has no module line", gomod)
	}
	if s.spec.Module == "" {
		s.spec.Module = s.mod.Module.Mod.Path
	} else if s.spec.Module != s.mod.Module.Mod.Path {
		return nil, fmt.Errorf("vuka ui: the library %s is module %s, but %s says module %s", spec.Name, s.spec.Module, gomod, s.mod.Module.Mod.Path)
	}
	return s, nil
}

// components are the library's components: the directories of its
// components directory holding a .templ file.
func (s *libSource) components() []string {
	entries, _ := os.ReadDir(filepath.Join(s.dir, filepath.FromSlash(s.spec.Components)))
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(s.dir, filepath.FromSlash(s.spec.Components), e.Name(), "*.templ"))
		if len(files) > 0 {
			out = append(out, e.Name())
		}
	}
	return out
}

// libPkg is a directory of the library the app gets: a component, a
// package the components import, or one of the shared scripts'.
type libPkg struct {
	name  string   // its directory in the app's components directory
	src   string   // its import path in the library
	dir   string   // its directory in the source
	files []string // the files written, by name
	js    bool     // it holds scripts
}

// pkgFiles are the files of a library directory the app gets: its
// sources, scripts and the files they embed, not the code templ generated
// (vuka compiles the .templ files), tests, or minified copies of scripts.
func pkgFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if !e.Type().IsRegular() || strings.HasPrefix(n, ".") || strings.HasSuffix(n, "_templ.go") ||
			strings.HasSuffix(n, "_test.go") || strings.HasSuffix(n, ".min.js") || strings.HasSuffix(n, "_templ.txt") {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

// destName is the directory of the app's components directory a package
// of the library goes to: a component's directory keeps its name, another
// package (utils) its last element.
func (s *libSource) destName(importPath string) (string, bool) {
	rel, ok := strings.CutPrefix(importPath, s.spec.Module+"/")
	if !ok {
		return "", false
	}
	if c, ok := strings.CutPrefix(rel, strings.Trim(s.spec.Components, "/")+"/"); ok {
		return c, true
	}
	return path.Base(rel), true
}

// collect is the packages the components named need: them, the packages
// of the library they import, and the shared scripts' when one has a
// script; and the modules outside it they import, with the versions the
// library requires.
func (s *libSource) collect(names []string) ([]*libPkg, map[string]string, error) {
	var out []*libPkg
	ext := map[string]string{}
	seen := map[string]*libPkg{}
	comps := strings.Trim(s.spec.Components, "/")
	var visit func(importPath, from string) error
	visit = func(importPath, from string) error {
		if seen[importPath] != nil {
			return nil
		}
		name, _ := s.destName(importPath)
		for _, p := range out {
			if p.name == name {
				return fmt.Errorf("vuka ui: %s and %s of %s would both be %s", p.src, importPath, s.spec.Name, name)
			}
		}
		rel := strings.TrimPrefix(importPath, s.spec.Module+"/")
		dir := filepath.Join(s.dir, filepath.FromSlash(rel))
		files, err := pkgFiles(dir)
		if err != nil || len(files) == 0 {
			if from == "" {
				all := s.components()
				return fmt.Errorf("vuka ui add: %s has no component %q%s; its components are %s", s.label(), name, suggest(name, all, ""), strings.Join(all, ", "))
			}
			return fmt.Errorf("vuka ui add: %s imports %s, which %s hasn't", from, importPath, s.label())
		}
		p := &libPkg{name: name, src: importPath, dir: dir, files: files}
		seen[importPath] = p
		out = append(out, p)
		for _, f := range files {
			switch filepath.Ext(f) {
			case ".js":
				p.js = true
			case ".go", ".templ":
				imps, err := goFileImports(filepath.Join(dir, f))
				if err != nil {
					return err
				}
				for _, imp := range imps {
					if _, ok := s.destName(imp); ok {
						if err := visit(imp, importPath); err != nil {
							return err
						}
						continue
					}
					if m := s.moduleOf(imp); m != nil {
						ext[m.Path] = m.Version
					}
				}
			}
		}
		return nil
	}
	for _, n := range names {
		if !libItemName.MatchString(n) {
			return nil, nil, fmt.Errorf("vuka ui add: %s/%s: a component's name is its directory's, in lower case", s.spec.Name, n)
		}
		if err := visit(s.spec.Module+"/"+comps+"/"+n, ""); err != nil {
			return nil, nil, err
		}
	}
	if slices.ContainsFunc(out, func(p *libPkg) bool { return p.js }) {
		for _, d := range s.spec.Scripts {
			if err := visit(s.spec.Module+"/"+comps+"/"+d, s.spec.Name+"'s scripts"); err != nil {
				return nil, nil, err
			}
		}
	}
	return out, ext, nil
}

// moduleOf is the module of the library's requirements an import path is
// in: nil for the standard library's, or one it doesn't require.
func (s *libSource) moduleOf(importPath string) *struct{ Path, Version string } {
	if first, _, _ := strings.Cut(importPath, "/"); !strings.Contains(first, ".") {
		return nil
	}
	var best *struct{ Path, Version string }
	for _, r := range s.mod.Require {
		p := r.Mod.Path
		if importPath == p || strings.HasPrefix(importPath, p+"/") {
			if best == nil || len(p) > len(best.Path) {
				best = &struct{ Path, Version string }{p, r.Mod.Version}
			}
		}
	}
	return best
}

// goFileImports are the import paths of a .go or .templ file: a .templ file
// starts as Go does, its package clause and imports.
func goFileImports(file string) ([]string, error) {
	src, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	f, err := parser.ParseFile(token.NewFileSet(), file, src, parser.ImportsOnly)
	if f == nil {
		return nil, fmt.Errorf("vuka ui: %v", err)
	}
	var out []string
	for _, imp := range f.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err == nil {
			out = append(out, p)
		}
	}
	return out, nil
}

var libItemName = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// importPathRe is a quoted import path in a .go or .templ file.
var importPathRe = regexp.MustCompile(`"([^"\s]+)"`)

// appFileOf is a library file as the app gets it: in a .go or .templ file,
// the library's packages imported from where the app has them.
func (s *libSource) appFileOf(dir, name, appImport string) ([]byte, error) {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return nil, err
	}
	if ext := filepath.Ext(name); ext != ".go" && ext != ".templ" {
		return b, nil
	}
	return importPathRe.ReplaceAllFunc(b, func(m []byte) []byte {
		if n, ok := s.destName(string(m[1 : len(m)-1])); ok {
			return []byte(strconv.Quote(appImport + "/" + n))
		}
		return m
	}), nil
}

// libArg is a vuka ui argument naming a library's component:
// library/name.
type libArg struct{ lib, name string }

// splitLibArgs splits vuka ui's arguments into vuka's components and
// libraries'.
func splitLibArgs(args []string) (own []string, libs []libArg, err error) {
	for _, a := range args {
		lib, name, ok := strings.Cut(strings.ToLower(a), "/")
		switch {
		case !ok:
			own = append(own, a)
		case lib == "" || name == "" || strings.Contains(name, "/"):
			return nil, nil, fmt.Errorf("vuka ui: %s: a library's component is library/name (shadcn-templ/dialog)", a)
		default:
			libs = append(libs, libArg{lib, name})
		}
	}
	return own, libs, nil
}

// libAdd is the part of a vuka ui add that libraries' components are:
// what it writes is queued on out; the modules to get are returned.
func libAdd(out *uiWriter, cfg uiConfig, modPath string, args []libArg, force bool) (gets map[string]string, err error) {
	lc, err := readUILib(out.root)
	if err != nil {
		return nil, err
	}
	gets = map[string]string{}
	var order []string
	byLib := map[string][]string{}
	for _, a := range args {
		if byLib[a.lib] == nil {
			order = append(order, a.lib)
		}
		byLib[a.lib] = append(byLib[a.lib], a.name)
	}
	appImport := modPath + "/" + cfg.Dir
	for _, name := range order {
		spec, err := lc.library(name)
		if err != nil {
			return nil, err
		}
		src, err := openLibrary(out.root, spec, "")
		if err != nil {
			return nil, err
		}
		pkgs, ext, err := src.collect(byLib[name])
		if err != nil {
			return nil, err
		}
		asked := map[string]bool{}
		for _, n := range byLib[name] {
			asked[n] = true
		}
		for _, p := range pkgs {
			if was, ok := lc.sources[p.name]; ok && libOf(was) != name {
				return nil, fmt.Errorf("vuka ui add: %s/%s is %s's (vuka.toml's [ui.sources]), not %s's", cfg.Dir, p.name, libOf(was), name)
			}
			for _, f := range p.files {
				want, err := src.appFileOf(p.dir, f, appImport)
				if err != nil {
					return nil, err
				}
				rel := path.Join(cfg.Dir, p.name, f)
				have, err := os.ReadFile(filepath.Join(out.root, filepath.FromSlash(rel)))
				switch {
				case errors.Is(err, os.ErrNotExist):
					out.write(rel, want, "")
				case err != nil:
					return nil, err
				case bytes.Equal(have, want):
					if asked[p.name] {
						out.note("%s is up to date", rel)
					}
				case asked[p.name] && force:
					out.write(rel, want, "")
				case asked[p.name]:
					out.differs(rel, have, want)
				default:
					out.note("%s is yours, differing from %s's (vuka ui diff %s/%s shows how)", rel, src.label(), name, p.name)
				}
			}
			lc.sources[p.name] = src.label()
		}
		if err := libTheme(out, cfg, src, lc, force); err != nil {
			return nil, err
		}
		for m, v := range ext {
			if have, ok := gets[m]; !ok || semver.Compare(have, v) < 0 {
				gets[m] = v
			}
			if v, ok := toolchainModules()[m]; ok {
				gets[m] = v
			}
		}
	}
	toml, err := os.ReadFile(filepath.Join(out.root, "vuka.toml"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if pending, ok := out.data["vuka.toml"]; ok {
		toml = pending
	}
	if next := setTOMLTable(toml, "ui.sources", lc.sourcesTable()); !bytes.Equal(next, toml) {
		out.write("vuka.toml", next, "[ui.sources]")
	}
	return gets, nil
}

// toolchainModules are the modules whose version the app must require as
// vuka builds with: templ, whose generator vuka links, and whose runtime
// the code it generates calls.
func toolchainModules() map[string]string {
	out := map[string]string{}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, d := range bi.Deps {
			if d.Path == "github.com/a-h/templ" {
				out[d.Path] = d.Version
			}
		}
	}
	if out["github.com/a-h/templ"] == "" {
		out["github.com/a-h/templ"] = templ.Version()
	}
	return out
}

func libOf(source string) string {
	lib, _, _ := strings.Cut(source, "@")
	return lib
}

func versionOf(source string) string {
	_, v, _ := strings.Cut(source, "@")
	return v
}

// sourcesTable is [ui.sources]'s text.
func (lc *uiLibConfig) sourcesTable() []byte {
	var b strings.Builder
	b.WriteString("[ui.sources]\n")
	for _, n := range slices.Sorted(mapKeys(lc.sources)) {
		fmt.Fprintf(&b, "%s = %q\n", n, lc.sources[n])
	}
	return []byte(b.String())
}

// setTOMLTable is toml with the table name's lines replaced by text, or
// with text added at its end.
func setTOMLTable(toml []byte, name string, text []byte) []byte {
	lines := strings.SplitAfter(string(toml), "\n")
	var out strings.Builder
	in, done := false, false
	for _, l := range lines {
		t := strings.TrimSpace(stripComment(l))
		if strings.HasPrefix(t, "[") {
			if in {
				out.WriteString("\n") // the blank line before the next table
			}
			in = strings.TrimSpace(strings.Trim(t, "[]")) == name
			if in {
				out.Write(text)
				done = true
				continue
			}
		}
		if !in {
			out.WriteString(l)
		}
	}
	if done {
		return []byte(out.String())
	}
	s := out.String()
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	if s != "" {
		s += "\n"
	}
	return []byte(s + string(text))
}

// getModules is go get of the modules the components import, at the
// versions their library requires, but for those the app requires as new
// already (or replaces): direct requirements.
func getModules(root string, gets map[string]string, w io.Writer) error {
	if len(gets) == 0 {
		return nil
	}
	m, err := readGoMod(root)
	if err != nil {
		return err
	}
	var args, paths []string
	for _, p := range slices.Sorted(mapKeys(gets)) {
		v := gets[p]
		if m.replaced(p) != "" {
			continue
		}
		have := m.requires(p)
		if have != "" && semver.Compare(have, v) >= 0 {
			if !directRequire(m, p) {
				paths = append(paths, p)
			}
			continue
		}
		args = append(args, p+"@"+v)
		paths = append(paths, p)
	}
	if len(args) > 0 {
		fmt.Fprintf(w, "go get %s\n", strings.Join(args, " "))
		c := exec.Command("go", append([]string{"get"}, args...)...)
		c.Dir = root
		var out bytes.Buffer
		c.Stdout, c.Stderr = &out, &out
		if err := c.Run(); err != nil {
			return fmt.Errorf("vuka ui add: go get %s: %v\n%s", strings.Join(args, " "), err, out.Bytes())
		}
	}
	return requireDirectly(root, paths)
}

// requireDirectly marks the app's requirements of the modules paths
// names direct: the app's own packages import them now.
func requireDirectly(root string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	file := filepath.Join(root, "go.mod")
	b, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	f, err := modfile.Parse(file, b, nil)
	if err != nil {
		return err
	}
	var reqs []*modfile.Require
	changed := false
	for _, r := range f.Require {
		nr := &modfile.Require{Mod: r.Mod, Indirect: r.Indirect}
		if r.Indirect && slices.Contains(paths, r.Mod.Path) {
			nr.Indirect, changed = false, true
		}
		reqs = append(reqs, nr)
	}
	if !changed {
		return nil
	}
	f.SetRequireSeparateIndirect(reqs)
	f.Cleanup()
	out, err := f.Format()
	if err != nil {
		return err
	}
	return os.WriteFile(file, out, 0o644)
}

func directRequire(m *goMod, p string) bool {
	for _, r := range m.Require {
		if r.Path == p {
			return !r.Indirect
		}
	}
	return false
}

// libDiff prints how the app's copies of the library components named
// (all [ui.sources] names when none are) differ from their libraries' at
// the version each came from, and returns those that differ.
func libDiff(root, modPath string, cfg uiConfig, args []libArg, all bool, w io.Writer) (differ, same []string, err error) {
	lc, err := readUILib(root)
	if err != nil {
		return nil, nil, err
	}
	type want struct{ dir, source string }
	var wants []want
	for _, a := range args {
		s, ok := lc.sources[a.name]
		if !ok || libOf(s) != a.lib {
			return nil, nil, fmt.Errorf("vuka ui diff: %s/%s came from no library (vuka.toml's [ui.sources] hasn't it as %s's)", cfg.Dir, a.name, a.lib)
		}
		wants = append(wants, want{a.name, s})
	}
	if all {
		for _, n := range slices.Sorted(mapKeys(lc.sources)) {
			wants = append(wants, want{n, lc.sources[n]})
		}
	}
	opened := map[string]*libSource{}
	var diffs bytes.Buffer
	for _, wt := range wants {
		src := opened[wt.source]
		if src == nil {
			spec, err := lc.library(libOf(wt.source))
			if err != nil {
				return nil, nil, err
			}
			if src, err = openLibrary(root, spec, versionOf(wt.source)); err != nil {
				return nil, nil, err
			}
			opened[wt.source] = src
		}
		label := libOf(wt.source) + "/" + wt.dir
		dir := filepath.Join(src.dir, filepath.FromSlash(src.spec.Components), wt.dir)
		if _, err := os.Stat(dir); err != nil {
			dir = findPkgDir(src, wt.dir)
		}
		files, err := pkgFiles(dir)
		if err != nil {
			return nil, nil, fmt.Errorf("vuka ui diff: %s hasn't %s", src.label(), wt.dir)
		}
		changed := false
		for _, f := range files {
			theirs, err := src.appFileOf(dir, f, modPath+"/"+cfg.Dir)
			if err != nil {
				return nil, nil, err
			}
			rel := path.Join(cfg.Dir, wt.dir, f)
			have, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			switch {
			case errors.Is(err, os.ErrNotExist):
				fmt.Fprintf(w, "%s: the app hasn't %s\n", label, rel)
				changed = true
			case err != nil:
				return nil, nil, err
			case !bytes.Equal(have, theirs):
				diffs.Write(unifiedText(rel, have, theirs))
				changed = true
			}
		}
		if changed {
			differ = append(differ, label)
		} else {
			same = append(same, label)
		}
	}
	w.Write(diffs.Bytes())
	return differ, same, nil
}

// findPkgDir is the directory of a package of the library outside its
// components directory that the app has as name (utils).
func findPkgDir(src *libSource, name string) string {
	if files, _ := filepath.Glob(filepath.Join(src.dir, name, "*.go")); len(files) > 0 {
		return filepath.Join(src.dir, name)
	}
	found, done := filepath.Join(src.dir, name), false
	filepath.WalkDir(src.dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || done {
			return filepath.SkipDir
		}
		if d.IsDir() && d.Name() == name {
			if files, _ := filepath.Glob(filepath.Join(p, "*.go")); len(files) > 0 {
				found, done = p, true
				return filepath.SkipDir
			}
		}
		if d.IsDir() && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		return nil
	})
	return found
}

// libList lists the components of a library, those the app has marked.
func libList(root, name string, w io.Writer) error {
	lc, err := readUILib(root)
	if err != nil {
		return err
	}
	spec, err := lc.library(name)
	if err != nil {
		return err
	}
	src, err := openLibrary(root, spec, "")
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "%s (%s):\n", src.label(), src.spec.Module)
	for _, c := range src.components() {
		mark := ""
		if s, ok := lc.sources[c]; ok && libOf(s) == name {
			mark = "  (the app has it: " + s + ")"
		}
		fmt.Fprintf(w, "  %s/%s%s\n", name, c, mark)
	}
	return nil
}

// listSources lists the components the app has from libraries.
func listSources(root string, w io.Writer) error {
	lc, err := readUILib(root)
	if err != nil || len(lc.sources) == 0 {
		return err
	}
	fmt.Fprintln(w, "\nFrom libraries (vuka.toml's [ui.sources]):")
	for _, n := range slices.Sorted(mapKeys(lc.sources)) {
		fmt.Fprintf(w, "  %-20s %s\n", libOf(lc.sources[n])+"/"+n, lc.sources[n])
	}
	return nil
}

// libraryScripts are the scripts of the libraries' components the app has
// ([ui.sources]): each library's bundled into one module, the shared
// scripts first and then each component's, in name order, loaded on the
// pages that show one of its components that has a script, whose markup
// carries the module's scope.
func (pa *pageAssets) libraryScripts() error {
	cfg, ok, err := readUIConfig(pa.root)
	if err != nil || !ok {
		return err
	}
	lc, err := readUILib(pa.root)
	if err != nil || len(lc.sources) == 0 {
		return err
	}
	byLib := map[string][]string{}
	for dir, s := range lc.sources {
		byLib[libOf(s)] = append(byLib[libOf(s)], dir)
	}
	base := filepath.Join(pa.root, filepath.FromSlash(cfg.Dir))
	for _, lib := range slices.Sorted(mapKeys(byLib)) {
		dirs := byLib[lib]
		slices.Sort(dirs)
		if spec, err := lc.library(lib); err == nil {
			first := slices.DeleteFunc(slices.Clone(spec.Scripts), func(d string) bool { return !slices.Contains(dirs, d) })
			dirs = append(first, slices.DeleteFunc(dirs, func(d string) bool { return slices.Contains(first, d) })...)
		}
		var c *assetComp
		for _, d := range dirs {
			dir := filepath.Join(base, d)
			scripts, _ := filepath.Glob(filepath.Join(dir, "*.js"))
			templs, _ := filepath.Glob(filepath.Join(dir, "*.templ"))
			var names []string
			for _, t := range templs {
				src, _ := os.ReadFile(t)
				for _, m := range templDeclRe.FindAllSubmatch(src, -1) {
					names = append(names, string(m[1]))
				}
			}
			for _, s := range scripts {
				if strings.HasSuffix(s, ".min.js") {
					continue
				}
				if c == nil {
					importPath := pa.modPath + "/" + cfg.Dir
					c = &assetComp{dir: base, importPath: importPath, name: lib, id: scopeID(importPath, "library:"+lib), marks: map[string][]string{}, classic: true}
					pa.comps = append(pa.comps, c)
				}
				r, _ := filepath.Rel(pa.root, s)
				c.scripts = append(c.scripts, assetSrc{path: s, label: filepath.ToSlash(r), dir: dir})
			}
			if c != nil && len(scripts) > 0 && len(names) > 0 {
				c.marks[dir] = append(c.marks[dir], names...)
			}
		}
	}
	return nil
}
