package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// vuka add's other kinds: a service, a component, an island, a page and an
// entity. Each writes its files through an adder: nothing is overwritten
// without --force, --dry-run lists what would change, and the files are
// formatted as vuka fmt formats them.

// addFlags are a vuka add command's arguments: its positionals and flags,
// in any order.
type addFlags struct {
	pos    []string
	bools  map[string]bool
	values map[string]string
}

// valueFlags take a value: --module notes, --dir=web/islands.
var valueFlags = map[string]bool{"module": true, "dir": true, "island": true, "table": true, "name": true}

var boolFlags = map[string]bool{"stateless": true, "unmanaged": true, "force": true, "dry-run": true, "live": true, "vue": true, "react": true, "svelte": true, "no-install": true}

func parseAddFlags(kind string, args []string) (*addFlags, error) {
	f := &addFlags{bools: map[string]bool{}, values: map[string]string{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" {
			f.pos = append(f.pos, a)
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(a, "-"), "=")
		switch {
		case boolFlags[name] && !hasValue:
			f.bools[name] = true
		case valueFlags[name]:
			if !hasValue {
				if i+1 >= len(args) {
					return nil, fmt.Errorf("vuka add %s: --%s needs a value", kind, name)
				}
				i++
				value = args[i]
			}
			f.values[name] = value
		default:
			return nil, fmt.Errorf("vuka add %s: no flag %s%s", kind, a, suggest(name, append(sortedKeys(boolFlags), sortedKeys(valueFlags)...), "--"))
		}
	}
	return f, nil
}

func sortedKeys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// suggest is ` (did you mean <prefix>x?)` for the closest of names to s.
func suggest(s string, names []string, prefix string) string {
	if best := closest(s, names); best != "" && editDistance(strings.ToLower(s), strings.ToLower(best)) <= 2 {
		return " (did you mean " + prefix + best + "?)"
	}
	return ""
}

// adder collects what a vuka add writes: new files and changed ones, all
// relative to the app's root.
type adder struct {
	kind    string
	root    string
	mod     string
	force   bool
	dryRun  bool
	files   map[string][]byte
	changed map[string][]byte
	order   []string
}

func newAdder(kind, root, mod string, f *addFlags) *adder {
	return &adder{kind: kind, root: root, mod: mod, force: f.bools["force"], dryRun: f.bools["dry-run"],
		files: map[string][]byte{}, changed: map[string][]byte{}}
}

// create adds a new file, formatted; one that exists is an error unless
// --force.
func (a *adder) create(rel string, src []byte) error {
	if _, dup := a.files[rel]; dup {
		return fmt.Errorf("vuka add %s: two files make %s", a.kind, rel)
	}
	if exists(a.root, filepath.FromSlash(rel)) && !a.force {
		return fmt.Errorf("vuka add %s: %s exists already (--force overwrites it)", a.kind, rel)
	}
	b, err := formatSource(rel, src, a.mod)
	if err != nil {
		return err
	}
	a.files[rel] = b
	a.order = append(a.order, rel)
	return nil
}

// change replaces an existing file's content.
func (a *adder) change(rel string, src []byte) {
	if _, ok := a.changed[rel]; !ok {
		a.order = append(a.order, rel)
	}
	a.changed[rel] = src
}

// commit writes the files (or, with --dry-run, writes nothing) and reports
// them on w.
func (a *adder) commit(w io.Writer) error {
	verb, change := "wrote", "changed"
	if a.dryRun {
		verb, change = "would write", "would change"
	}
	for _, rel := range a.order {
		if a.dryRun {
			continue
		}
		b, ok := a.files[rel]
		if !ok {
			b = a.changed[rel]
		}
		p := filepath.Join(a.root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			return err
		}
	}
	for _, rel := range a.order {
		if _, ok := a.files[rel]; ok {
			fmt.Fprintln(w, verb, rel)
		} else {
			fmt.Fprintln(w, change, rel)
		}
	}
	return nil
}

// render executes the add template name (scaffold/add/name.tmpl) with data.
func render(name string, data any) ([]byte, error) {
	src, err := scaffoldFS.ReadFile("scaffold/add/" + name + ".tmpl")
	if err != nil {
		return nil, err
	}
	return execute(name, src, data)
}

var exportedName = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

// checkTypeName checks a Go type's name given to vuka add.
func checkTypeName(kind, name, what string) error {
	switch {
	case name == "":
		return fmt.Errorf("vuka add %s: give the %s's name", kind, what)
	case !token.IsIdentifier(name) || !exportedName.MatchString(name):
		fix := ""
		if token.IsIdentifier(name) || regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`).MatchString(name) {
			fix = " (" + camel(name) + "?)"
		}
		return fmt.Errorf("vuka add %s %s: a %s's name is an exported Go identifier, letters and digits starting upper-case: Invoice, UserCard%s", kind, name, what, fix)
	}
	return nil
}

// camel is s (snake_case, kebab-case or camelCase) in UpperCamelCase.
func camel(s string) string {
	var b strings.Builder
	up := true
	for _, r := range s {
		if r == '_' || r == '-' || r == ' ' || r == '/' {
			up = true
			continue
		}
		if up {
			r = unicode.ToUpper(r)
		}
		up = false
		b.WriteRune(r)
	}
	return b.String()
}

// snake is a CamelCase name in snake_case: UserCard is user_card.
func snake(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i, r := range rs {
		if unicode.IsUpper(r) {
			if i > 0 && (unicode.IsLower(rs[i-1]) || i+1 < len(rs) && unicode.IsLower(rs[i+1]) && unicode.IsUpper(rs[i-1])) {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

// lowerFirst is s with its first letter lower-cased: what JSON and the
// wire call a Go name.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// plural is a noun's plural, for a table's name: invoice is invoices.
func plural(s string) string {
	switch {
	case strings.HasSuffix(s, "y") && len(s) > 1 && !strings.ContainsRune("aeiou", rune(s[len(s)-2])):
		return s[:len(s)-1] + "ies"
	case strings.HasSuffix(s, "s"), strings.HasSuffix(s, "x"), strings.HasSuffix(s, "ch"), strings.HasSuffix(s, "sh"):
		return s + "es"
	}
	return s + "s"
}

var declRe = regexp.MustCompile(`(?m)^(?:func|type|var|const)\s+([A-Za-z_]\w*)\b`)

// declared reports whether the package in dir declares name at its top
// level (funcs, types, vars and consts of its .vuka and .go files).
func declared(dir, name string) string {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !(strings.HasSuffix(n, ".vuka") || strings.HasSuffix(n, ".vuka.html") || strings.HasSuffix(n, ".go")) || strings.HasSuffix(n, "_vuka.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		for _, m := range declRe.FindAllSubmatch(src, -1) {
			if string(m[1]) == name {
				return n
			}
		}
		// var ( … ) and const ( … ) blocks, roughly
		if regexp.MustCompile(`(?m)^\t`+regexp.QuoteMeta(name)+`\s*(=|[A-Za-z\[*])`).Match(src) && bytes.Contains(src, []byte("\n)")) {
			if regexp.MustCompile(`(?ms)^(?:var|const) \(\n.*?^\t` + regexp.QuoteMeta(name) + `\b.*?^\)`).Match(src) {
				return n
			}
		}
	}
	return ""
}

// target is where a kind's Go file goes: the module's package
// (modules/<m>) or the app's own directory for it (components, pages).
type target struct {
	dir, pkg string // relative to the root (slash form), the package's name
}

func (a *adder) moduleTarget(module, own string) (target, error) {
	if module == "" {
		return target{dir: own, pkg: own}, nil
	}
	if !moduleName.MatchString(module) {
		return target{}, fmt.Errorf("vuka add %s: --module %s: a module's name is lower-case letters and digits", a.kind, module)
	}
	if !exists(a.root, "modules", module) {
		return target{}, fmt.Errorf("vuka add %s: the app has no module %s%s (vuka add module %s makes one)", a.kind, module, suggest(module, moduleNames(a.root), ""), module)
	}
	return target{dir: "modules/" + module, pkg: module}, nil
}

func moduleNames(root string) []string {
	entries, _ := os.ReadDir(filepath.Join(root, "modules"))
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// register imports the package at dir (relative to root) in main.vuka when
// nothing in the app imports it yet, so its declarations take effect.
func (a *adder) register(dir string) error {
	pkg := a.mod + "/" + dir
	main, err := os.ReadFile(filepath.Join(a.root, "main.vuka"))
	if err != nil {
		return nil // an app without a main.vuka wires itself
	}
	if bytes.Contains(main, []byte(strconv.Quote(pkg))) || importedAnywhere(a.root, pkg) {
		return nil
	}
	out, err := registerImport(main, a.mod, pkg)
	if err != nil {
		return err
	}
	a.change("main.vuka", out)
	return nil
}

// registerService registers the service ctor of the package at dir
// (relative to root) in main.vuka's services function (registerService);
// false when main.vuka has none, the package then imported as register
// does.
func (a *adder) registerService(dir, pkg, ctor string) (bool, error) {
	path := a.mod + "/" + dir
	main, err := os.ReadFile(filepath.Join(a.root, "main.vuka"))
	if err != nil {
		return false, nil // an app without a main.vuka wires itself
	}
	if src, ok := a.changed["main.vuka"]; ok {
		main = src
	}
	out, ok, err := registerService(main, a.mod, path, pkg, ctor)
	if err != nil || !ok {
		return false, errors.Join(err, a.register(dir))
	}
	a.change("main.vuka", out)
	return true, nil
}

func importedAnywhere(root, pkg string) bool {
	found := false
	walkSources(root, func(p, name string) error {
		if found || !(strings.HasSuffix(name, ".vuka") || strings.HasSuffix(name, ".go")) || strings.HasSuffix(name, "_test.vuka") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		if src, err := os.ReadFile(p); err == nil && bytes.Contains(src, []byte(strconv.Quote(pkg))) {
			found = true
		}
		return nil
	})
	return found
}

// moduleAndName is the module and the name of <Name> [--module m], or of
// the shorter <module>/<Name>; no module is the app's own.
func moduleAndName(kind, arg string, f *addFlags) (string, string, error) {
	m, n, ok := strings.Cut(arg, "/")
	if !ok {
		return f.values["module"], arg, nil
	}
	if m == "" || n == "" {
		return "", "", fmt.Errorf("vuka add %s %s: <Name> [--module m], or <module>/<Name>", kind, arg)
	}
	if fm := f.values["module"]; fm != "" && fm != m {
		return "", "", fmt.Errorf("vuka add %s %s: --module %s names another module", kind, arg, fm)
	}
	return m, n, nil
}

// ---- service ----

type serviceSpec struct {
	Pkg, Name, Recv string
}

func addService(root, mod string, f *addFlags, w io.Writer) error {
	if len(f.pos) != 1 {
		return errors.New("usage: vuka add service <Name> [--module m] [--force] [--dry-run]")
	}
	a := newAdder("service", root, mod, f)
	module, name, err := moduleAndName("service", f.pos[0], f)
	if err != nil {
		return err
	}
	if err := checkTypeName("service", name, "service"); err != nil {
		return err
	}
	t, err := a.moduleTarget(module, "services")
	if err != nil {
		return err
	}
	if at := declared(filepath.Join(root, t.dir), name); at != "" && !a.force {
		return fmt.Errorf("vuka add service: %s is declared already in %s/%s", name, t.dir, at)
	}
	spec := serviceSpec{Pkg: t.pkg, Name: name, Recv: strings.ToLower(name[:1])}
	for _, f := range [][2]string{{"service.vuka", snake(name) + ".vuka"}, {"service_test.vuka", snake(name) + "_test.vuka"}} {
		src, err := render(f[0], spec)
		if err != nil {
			return err
		}
		if err := a.create(t.dir+"/"+f[1], src); err != nil {
			return err
		}
	}
	registered, err := a.registerService(t.dir, t.pkg, "New"+name)
	if err != nil {
		return err
	}
	if err := a.commit(w); err != nil {
		return err
	}
	if !registered {
		fmt.Fprintf(w, "\nRegister it in main: app.Service(%s.New%s). ", t.pkg, name)
	} else {
		fmt.Fprint(w, "\n")
	}
	fmt.Fprintf(w, "%s is a dependency now: take a *%s.%s parameter where you need it. Then:\n\n\tvuka test ./%s\n", name, t.pkg, name, t.dir)
	return nil
}

// ---- component ----

type componentSpec struct {
	Pkg, Name string
}

func addComponent(root, mod string, f *addFlags, w io.Writer) error {
	if len(f.pos) != 1 {
		return errors.New("usage: vuka add component <Name> [--module m] [--force] [--dry-run]")
	}
	a := newAdder("component", root, mod, f)
	name := f.pos[0]
	if err := checkTypeName("component", name, "component"); err != nil {
		return err
	}
	t, err := a.moduleTarget(f.values["module"], "components")
	if err != nil {
		return err
	}
	for _, n := range []string{name, name + "Props"} {
		if at := declared(filepath.Join(root, t.dir), n); at != "" && !a.force {
			return fmt.Errorf("vuka add component: %s is declared already in %s/%s", n, t.dir, at)
		}
	}
	src, err := render("component.vuka.html", componentSpec{Pkg: t.pkg, Name: name})
	if err != nil {
		return err
	}
	if err := a.create(t.dir+"/"+snake(name)+".vuka.html", src); err != nil {
		return err
	}
	if err := a.commit(w); err != nil {
		return err
	}
	q := t.pkg + "."
	fmt.Fprintf(w, "\nUse it as a tag (import %q):\n\n\t<%s%s title=\"Hello\">…</%s%s>\n", mod+"/"+t.dir, q, name, q, name)
	return nil
}

// ---- island ----

// frameworks are the islands' frameworks: the component file's extension,
// the npm packages it builds with, and the Vite plugin.
var frameworks = map[string]struct {
	ext                string
	deps, devDeps      map[string]string
	pluginImport, call string
}{
	"vue": {".vue", map[string]string{"vue": "^3.5.44"}, map[string]string{"@vitejs/plugin-vue": "^6.0.10", "typescript": "~6.0.3"},
		"import vue from '@vitejs/plugin-vue'", "vue()"},
	"react": {".tsx", map[string]string{"react": "^19.3.0", "react-dom": "^19.3.0"}, map[string]string{"@vitejs/plugin-react": "^6.1.2", "@types/react": "^19.2.0"},
		"import react from '@vitejs/plugin-react'", "react()"},
	"svelte": {".svelte", map[string]string{"svelte": "^5.57.2"}, map[string]string{"@sveltejs/vite-plugin-svelte": "^7.3.1"},
		"import { svelte } from '@sveltejs/vite-plugin-svelte'", "svelte()"},
}

// islandSpec is what the island templates read.
type islandSpec struct {
	Name     string
	Live     bool   // a page-backed component reading its struct's fields
	Page     string // the page's struct, for its types
	Prop     string // the sample prop's json name
	URLField string // the bind: url: field's json name
}

// islandRoot is the app's island frontend root (a Vite project with a
// package.json), relative to root.
func islandRoot(root string) (string, error) {
	fr := findFrontendRoot(root)
	if fr == root || !fileExists(filepath.Join(fr, "package.json")) {
		return "", errors.New("vuka add island: the app has no island frontend (a Vite project its web.Frontend or web.IslandRoot names); vuka new --islands=yes makes an app with one")
	}
	r, _ := filepath.Rel(root, fr)
	return filepath.ToSlash(r), nil
}

// framework is the flag naming one, else the one the frontend uses, else Vue.
func pickFramework(f *addFlags, rootDir string) (string, error) {
	var picked []string
	for _, fw := range []string{"vue", "react", "svelte"} {
		if f.bools[fw] {
			picked = append(picked, fw)
		}
	}
	if len(picked) > 1 {
		return "", fmt.Errorf("vuka add: one framework: --%s", strings.Join(picked, " or --"))
	}
	if len(picked) == 1 {
		return picked[0], nil
	}
	if pkg, err := os.ReadFile(filepath.Join(rootDir, "package.json")); err == nil {
		for _, fw := range []string{"vue", "react", "svelte"} {
			if bytes.Contains(pkg, []byte(`"`+fw+`"`)) {
				return fw, nil
			}
		}
	}
	return "vue", nil
}

// island writes the component of an island named name, and what its
// framework needs in the frontend: it returns the component's path.
func (a *adder) island(name, dir, fw string, spec islandSpec, w io.Writer) (string, error) {
	fr, err := islandRoot(a.root)
	if err != nil {
		return "", err
	}
	if dir == "" {
		dir = fr + "/islands"
	}
	dir = filepath.ToSlash(filepath.Clean(dir))
	if dir != fr && !strings.HasPrefix(dir, fr+"/") {
		return "", fmt.Errorf("vuka add %s: --dir %s is outside the island frontend %s/: its build reaches only that", a.kind, dir, fr)
	}
	fwk := frameworks[fw]
	file := dir + "/" + name + fwk.ext
	for _, other := range []string{".vue", ".tsx", ".jsx", ".svelte", ".ts", ".js"} {
		if o := dir + "/" + name + other; other != fwk.ext && exists(a.root, filepath.FromSlash(o)) && !a.force {
			return "", fmt.Errorf("vuka add %s: %s exists already: an island %s (--force writes %s beside it)", a.kind, o, name, file)
		}
	}
	src, err := render("island"+fwk.ext, spec)
	if err != nil {
		return "", err
	}
	if err := a.create(file, src); err != nil {
		return "", err
	}
	if err := a.frameworkSetup(fr, fw); err != nil {
		return "", err
	}
	if ts := fr + "/tsconfig.json"; !exists(a.root, filepath.FromSlash(ts)) {
		if err := a.create(ts, liveTSConfig()); err != nil {
			return "", err
		}
	}
	return file, nil
}

// frameworkSetup adds the framework's packages to the frontend's
// package.json and its Vite plugin to vite.config, when they are missing.
func (a *adder) frameworkSetup(fr, fw string) error {
	fwk := frameworks[fw]
	pkgRel := fr + "/package.json"
	data, err := os.ReadFile(filepath.Join(a.root, filepath.FromSlash(pkgRel)))
	if err != nil {
		return err
	}
	var pkg map[string]any
	if err := json.Unmarshal(data, &pkg); err != nil {
		return fmt.Errorf("vuka add %s: %s: %v", a.kind, pkgRel, err)
	}
	has := func(name string) bool {
		for _, k := range []string{"dependencies", "devDependencies"} {
			if m, ok := pkg[k].(map[string]any); ok && m[name] != nil {
				return true
			}
		}
		return false
	}
	var adds [][3]string
	for _, set := range []struct {
		key  string
		deps map[string]string
	}{{"dependencies", fwk.deps}, {"devDependencies", fwk.devDeps}} {
		for _, n := range sortedKeys(boolsOf(set.deps)) {
			if !has(n) {
				adds = append(adds, [3]string{set.key, n, set.deps[n]})
			}
		}
	}
	if len(adds) > 0 {
		out, err := addPackages(data, adds)
		if err != nil {
			return fmt.Errorf("vuka add %s: %s: %v", a.kind, pkgRel, err)
		}
		a.change(pkgRel, out)
	}
	for _, n := range []string{"vite.config.mts", "vite.config.ts", "vite.config.mjs", "vite.config.js"} {
		rel := fr + "/" + n
		src, err := os.ReadFile(filepath.Join(a.root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		pluginPkg := strings.Trim(fwk.pluginImport[strings.LastIndex(fwk.pluginImport, " ")+1:], "'")
		if bytes.Contains(src, []byte(pluginPkg)) {
			break
		}
		out, ok := addVitePlugin(src, fwk.pluginImport, fwk.call)
		if !ok {
			return fmt.Errorf("vuka add %s: add %s to %s's plugins (and %s)", a.kind, fwk.call, rel, fwk.pluginImport)
		}
		a.change(rel, out)
		break
	}
	return nil
}

func boolsOf(m map[string]string) map[string]bool {
	out := map[string]bool{}
	for k := range m {
		out[k] = true
	}
	return out
}

// addPackages adds name: version entries to package.json's sections,
// keeping the file's order and indentation.
func addPackages(src []byte, adds [][3]string) ([]byte, error) {
	s := string(src)
	for _, add := range adds {
		key, name, version := add[0], add[1], add[2]
		entry := strconv.Quote(name) + ": " + strconv.Quote(version)
		re := regexp.MustCompile(`(?s)("` + key + `"\s*:\s*\{)(\s*)(.*?)(\n?\s*\})`)
		m := re.FindStringSubmatchIndex(s)
		if m == nil {
			// a new section before the closing brace
			end := strings.LastIndex(s, "}")
			if end < 0 {
				return nil, errors.New("not a JSON object")
			}
			body := strings.TrimRight(s[:end], " \t\n")
			sep := ","
			if strings.HasSuffix(body, "{") {
				sep = ""
			}
			s = body + sep + "\n  " + strconv.Quote(key) + ": {\n    " + entry + "\n  }\n" + s[end:]
			continue
		}
		indent := s[m[4]:m[5]]
		if strings.TrimSpace(s[m[6]:m[7]]) == "" {
			s = s[:m[3]] + "\n    " + entry + "\n  " + s[m[7]:]
			continue
		}
		s = s[:m[3]] + indent + entry + "," + s[m[3]:]
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, err
	}
	return []byte(s), nil
}

var pluginsRe = regexp.MustCompile(`plugins\s*:\s*\[`)

// addVitePlugin imports a plugin into a vite.config and puts its call
// first among the plugins.
func addVitePlugin(src []byte, imp, call string) ([]byte, bool) {
	loc := pluginsRe.FindIndex(src)
	if loc == nil {
		return nil, false
	}
	s := string(src[:loc[1]]) + call + ", " + string(src[loc[1]:])
	lines := strings.SplitAfter(s, "\n")
	last := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "import ") {
			last = i
		}
	}
	lines = slices.Insert(lines, last+1, imp+"\n")
	return []byte(strings.Join(lines, "")), true
}

// install installs the frontend's packages after package.json changed,
// showing it is under way (unless --no-install).
func (a *adder) install(fr string, f *addFlags, w io.Writer) error {
	rel := fr + "/package.json"
	if _, ok := a.changed[rel]; !ok || a.dryRun {
		return nil
	}
	dir := filepath.Join(a.root, filepath.FromSlash(fr))
	cmd := installCommand(dir)
	if cmd[len(cmd)-1] == "ci" || strings.Contains(strings.Join(cmd, " "), "frozen") || strings.Contains(strings.Join(cmd, " "), "immutable") {
		cmd = []string{cmd[0], "install"} // package.json changed: the lockfile follows it
	}
	if f.bools["no-install"] {
		fmt.Fprintf(w, "\nInstall the new packages: (cd %s && %s)\n", fr, strings.Join(cmd, " "))
		return nil
	}
	if _, err := exec.LookPath(cmd[0]); err != nil {
		fmt.Fprintf(w, "\n%s isn't installed: install Node.js 20+, then (cd %s && %s)\n", cmd[0], fr, strings.Join(cmd, " "))
		return nil
	}
	if err := checkNPM(cmd, fr+"/", w); err != nil {
		return fmt.Errorf("%w\n(the files are written; run (cd %s && %s) after upgrading)", err, fr, strings.Join(cmd, " "))
	}
	c := exec.Command(cmd[0], cmd[1:]...)
	c.Dir = dir
	if err := installTask(w, fr, cmd).Run(c); err != nil {
		return fmt.Errorf("vuka add %s: %s in %s: %v (the files are written; run it again)", a.kind, strings.Join(cmd, " "), fr, err)
	}
	return nil
}

// relImport is an import of file (relative to root) from a .vuka file in
// dir (relative to root): "./…" or "../…".
func relImport(dir, file string) string {
	r, _ := filepath.Rel(filepath.FromSlash(dir), filepath.FromSlash(file))
	r = filepath.ToSlash(r)
	if !strings.HasPrefix(r, ".") {
		r = "./" + r
	}
	return r
}

func addIsland(root, mod string, f *addFlags, w io.Writer) error {
	if len(f.pos) != 1 {
		return errors.New("usage: vuka add island <Name> [--module m] [--vue|--react|--svelte] [--dir dir] [--no-install] [--force] [--dry-run]")
	}
	a := newAdder("island", root, mod, f)
	name := f.pos[0]
	if err := checkTypeName("island", name, "island"); err != nil {
		return err
	}
	fr, err := islandRoot(root)
	if err != nil {
		return err
	}
	fw, err := pickFramework(f, filepath.Join(root, fr))
	if err != nil {
		return err
	}
	module := f.values["module"]
	t, err := a.moduleTarget(module, "components")
	if err != nil {
		return err
	}
	for _, n := range []string{name, name + "Props", name + "Island"} {
		if at := declared(filepath.Join(root, t.dir), n); at != "" && !a.force {
			return fmt.Errorf("vuka add island: %s is declared already in %s/%s", n, t.dir, at)
		}
	}
	dir := f.values["dir"]
	if dir == "" && module != "" {
		dir = fr + "/islands/" + module
	}
	file, err := a.island(name, dir, fw, islandSpec{Name: name}, w)
	if err != nil {
		return err
	}
	src, err := render("island_host.vuka.html", islandHostSpec{Pkg: t.pkg, Name: name, Import: relImport(t.dir, file), File: file})
	if err != nil {
		return err
	}
	if err := a.create(t.dir+"/"+snake(name)+".vuka.html", src); err != nil {
		return err
	}
	if err := a.commit(w); err != nil {
		return err
	}
	if err := a.install(fr, f, w); err != nil {
		return err
	}
	fmt.Fprintf(w, "\nPlace it in %s/ as\n\n\t<%s title=\"Hello\" count={3} />\n\nand elsewhere (import %q) as\n\n\t<%s.%s title=\"Hello\" count={3} />\n", t.dir, name, mod+"/"+t.dir, t.pkg, name)
	return nil
}

// inferredAt reports whether comp (slash-form, relative to root, about to be
// written) is the one component a bare @ui.Island on name, declared in dir,
// finds.
func inferredAt(root, fr, dir, name, comp string) bool {
	base := path.Join(fr, "islands", islandModule(dir+"/x.vuka"), name)
	if strings.TrimSuffix(comp, path.Ext(comp)) != base {
		return false
	}
	for _, ext := range islandExts {
		if o := base + ext; o != comp && exists(root, filepath.FromSlash(o)) {
			return false
		}
	}
	return true
}

type islandHostSpec struct {
	Pkg, Name, Import, File string
}

// ---- page ----

type pageSpec struct {
	Module, Pkg, Type, Path, Title string
	Params                         []pageParam
	Live, Document                 bool // live; the page writes its own <html> (no pages/_layout.vuka.html)
	Island, IslandPath             string
	InferIsland                    bool // the component is where a bare @ui.Island looks
	Prop, PropJSON                 string
	URLField, URLJSON              string
}

type pageParam struct{ Name, Field string }

var (
	pathSegment  = regexp.MustCompile(`^(\{[a-z][A-Za-z0-9]*\}|[a-z0-9][a-z0-9._-]*)$`)
	groupSegment = regexp.MustCompile(`^\([A-Za-z_][A-Za-z0-9_]*\)$`) // a route group: a folder, no part of the URL
)

func addPage(root, mod string, f *addFlags, w io.Writer) error {
	if len(f.pos) != 1 {
		return errors.New("usage: vuka add page <path> [--live|--stateless] [--island Name [--module m] [--vue|--react|--svelte]] [--name Type] [--force] [--dry-run]")
	}
	a := newAdder("page", root, mod, f)
	path := f.pos[0]
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("vuka add page %s: a page's path starts with /: /%s", path, path)
	}
	var words, segs, url []string // the path's fixed words; its segments as files and folders name them; as the URL does
	var params []pageParam
	for _, seg := range strings.Split(strings.Trim(path, "/"), "/") {
		if seg == "" && path == "/" {
			break
		}
		if groupSegment.MatchString(seg) {
			segs = append(segs, seg)
			continue
		}
		url = append(url, seg)
		if !pathSegment.MatchString(seg) {
			return fmt.Errorf("vuka add page %s: %q isn't a path segment: lower-case letters, digits, . _ -, a {param}, or a (group)", path, seg)
		}
		if p := strings.Trim(seg, "{}"); p != seg {
			field := camel(p)
			if strings.HasSuffix(field, "Id") {
				field = strings.TrimSuffix(field, "Id") + "ID"
			}
			params = append(params, pageParam{Name: p, Field: field})
			segs = append(segs, "["+p+"]")
			continue
		}
		words = append(words, strings.NewReplacer(".", "_", "-", "_").Replace(seg))
		segs = append(segs, seg)
	}
	if f.bools["live"] && f.bools["stateless"] {
		return fmt.Errorf("vuka add page %s: --live or --stateless, not both", path)
	}
	path = "/" + strings.Join(url, "/") // its URL: the (groups) are folders alone
	// live unless --stateless; a path with {parameters} only with --live
	live := !f.bools["stateless"] && (f.bools["live"] || len(params) == 0)
	island := f.values["island"]
	if len(params) > 0 && island != "" {
		return fmt.Errorf("vuka add page %s: a component-backed page from a path with {parameters} is yours to write; vuka add page makes it without them", path)
	}
	if f.values["module"] != "" && island == "" {
		return fmt.Errorf("vuka add page %s: a page's file is its path under pages/, so --module goes with --island Name (the component's folder)", path)
	}
	typ := f.values["name"]
	if typ == "" {
		if len(words) == 0 {
			return fmt.Errorf("vuka add page %s: name its type: --name Home", path)
		}
		typ = camel(strings.Join(words, "_"))
		if !token.IsIdentifier(typ) || !exportedName.MatchString(typ) {
			return fmt.Errorf("vuka add page %s: name its type: --name %s", path, camel(strings.Map(func(r rune) rune {
				if unicode.IsLetter(r) || unicode.IsDigit(r) {
					return r
				}
				return '_'
			}, strings.Join(words, "_"))))
		}
		if island == "" {
			typ += "Page"
		}
	}
	if err := checkTypeName("page", typ, "page"); err != nil {
		return err
	}
	if f.values["module"] != "" {
		if _, err := a.moduleTarget(f.values["module"], ""); err != nil {
			return err
		}
	}
	// The page's file is its path under pages/: /reports/{id} is
	// pages/reports/[id].vuka.html, / and a folder's own path its index.
	dir, base := "pages", "index"
	if len(segs) > 0 {
		dir, base = "pages/"+strings.Join(segs[:len(segs)-1], "/"), segs[len(segs)-1]
		if fi, err := os.Stat(filepath.Join(root, "pages", filepath.FromSlash(strings.Join(segs, "/")))); err == nil && fi.IsDir() || groupSegment.MatchString(base) {
			dir, base = "pages/"+strings.Join(segs, "/"), "index"
		}
	}
	dir = strings.TrimSuffix(dir, "/")
	if at := declared(filepath.Join(root, dir), typ); at != "" && !a.force {
		return fmt.Errorf("vuka add page: %s is declared already in %s/%s (--name names another)", typ, dir, at)
	}
	if other := routeTaken(root, path); other != "" && !a.force {
		return fmt.Errorf("vuka add page: %s is a route already, in %s", path, other)
	}
	spec := pageSpec{Module: mod, Pkg: pagePackage(root, dir), Type: typ, Path: path, Params: params, Title: displayName(strings.Join(words, " ")), Live: live,
		Document: !exists(root, "pages", "_layout.vuka.html")}
	if spec.Title == "" {
		spec.Title = typ
	}
	file := dir + "/" + base + ".vuka.html"
	tmpl := "page.vuka.html"
	if island != "" {
		if err := checkTypeName("page", island, "island"); err != nil {
			return err
		}
		if f.values["name"] != "" && island != typ {
			return fmt.Errorf("vuka add page: --island %s backs the page, so the page's type is %s: leave --name out", island, island)
		}
		typ, spec.Type = island, island
		file = dir + "/" + base + ".vuka"
		fr, err := islandRoot(root)
		if err != nil {
			return err
		}
		fw, err := pickFramework(f, filepath.Join(root, fr))
		if err != nil {
			return err
		}
		spec.Prop, spec.PropJSON, spec.URLField, spec.URLJSON = "Title", "title", "Q", "q"
		cdir := f.values["dir"]
		if cdir == "" && f.values["module"] != "" {
			cdir = fr + "/islands/" + f.values["module"]
		}
		comp, err := a.island(island, cdir, fw, islandSpec{Name: island, Live: true, Page: island, Prop: "title", URLField: "q"}, w)
		if err != nil {
			return err
		}
		spec.Island, spec.IslandPath = island, strings.TrimPrefix(comp, fr+"/")
		spec.InferIsland = inferredAt(root, fr, dir, island, comp)
		tmpl = "page_island.vuka"
		defer func() { a.install(fr, f, w) }()
	} else if f.bools["vue"] || f.bools["react"] || f.bools["svelte"] {
		return errors.New("vuka add page: --vue, --react and --svelte go with --island Name")
	}
	if exists(root, filepath.FromSlash(file)) && !a.force {
		return fmt.Errorf("vuka add page: %s exists already: it is the page at %s (--force overwrites it)", file, path)
	}
	src, err := render(tmpl, spec)
	if err != nil {
		return err
	}
	if err := a.create(file, src); err != nil {
		return err
	}
	routes := pagesRoutesEdit(root, mod, filepath.Join(root, pagesDirName))
	if routes.Src != nil {
		a.change(filepath.ToSlash(routes.File), routes.Src)
	}
	if err := a.commit(w); err != nil {
		return err
	}
	if routes.Hint != "" {
		fmt.Fprintf(w, "\n%s\n", routes.Hint)
	}
	example := strings.NewReplacer("{", "", "}", "").Replace(regexp.MustCompile(`\{[^}]+\}`).ReplaceAllString(path, "1"))
	fmt.Fprintf(w, "\nRun the app (vuka run -watch .) and open http://localhost:8080%s\n", example)
	return nil
}

// pagePackage is the package name of the pages folder dir (relative to
// root): its files', else its name's (a bracket folder's, its
// parameter's).
func pagePackage(root, dir string) string {
	entries, _ := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || strings.Contains(n, "_test.") || !(strings.HasSuffix(n, ".go") || strings.HasSuffix(n, ".vuka") || strings.HasSuffix(n, ".vuka.html")) {
			continue
		}
		if src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(dir), n)); err == nil {
			if name := packageName(n, src); name != "" {
				return name
			}
		}
	}
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, strings.ToLower(strings.TrimSuffix(path.Base(dir), "...")))
	if name == "" || token.IsKeyword(name) || unicode.IsDigit(rune(name[0])) {
		return "page"
	}
	return name
}

var routeRe = regexp.MustCompile(`@web\.(?:Get|Live|Post|Handle)\("([^"]*)"`)

// routeTaken is the file declaring a route at path, if one does.
func routeTaken(root, path string) string {
	found := ""
	walkSources(root, func(p, name string) error {
		if found != "" || !strings.HasSuffix(name, ".vuka") || strings.HasSuffix(name, "_test.vuka") {
			return nil
		}
		src, _ := os.ReadFile(p)
		for _, m := range routeRe.FindAllSubmatch(src, -1) {
			if string(m[1]) == path {
				r, _ := filepath.Rel(root, p)
				found = filepath.ToSlash(r)
			}
		}
		return nil
	})
	return found
}

// ---- entity ----

// entityField is a model's field from name:type[:unique|:index].
type entityField struct {
	Name, Column, JSON string // Go, SQL, JSON
	Type               string // Go
	Attrs              string // vuka attributes after the field
	Sample             string // a Go value for the test, "" for none
	Ref                string // a reference's model (Go, qualified when another package's)
	RefImport          string
	Time               bool
	Null               bool
}

type entitySpec struct {
	Pkg, Name, Table string
	Fields           []entityField
	Imports          []string
	Time             bool
	Refs             []string // the referenced models' managers, for the test
	Testable         bool     // the test creates a row (no required reference)
	SampleTime       bool     // its row has a time
	Unmanaged        bool     // an existing table: no timestamps, no migration
}

var entityTypes = []string{"string", "text", "int", "bigint", "float", "decimal", "bool", "time", "date", "ref", "json"}

var fieldName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func parseEntityField(root, mod, pkgDir string, arg string) (entityField, error) {
	parts := strings.Split(arg, ":")
	if len(parts) < 2 {
		return entityField{}, fmt.Errorf("vuka add entity: %s: a field is name:type, as in title:string (types: %s)", arg, strings.Join(entityTypes, ", "))
	}
	name, typ, mods := parts[0], parts[1], parts[2:]
	null := strings.HasSuffix(typ, "?")
	typ = strings.TrimSuffix(typ, "?")
	if !fieldName.MatchString(name) {
		return entityField{}, fmt.Errorf("vuka add entity: %s: a field's name is snake_case: %s", arg, strings.ToLower(snake(camel(name))))
	}
	f := entityField{Column: name, Null: null}
	if typ == "ref" {
		name = strings.TrimSuffix(name, "_id")
	}
	f.Name = camel(name)
	if strings.HasSuffix(f.Name, "Id") {
		f.Name = strings.TrimSuffix(f.Name, "Id") + "ID"
	}
	f.JSON = lowerFirst(f.Name)
	if f.Name == "ID" || f.Name == "CreatedAt" || f.Name == "UpdatedAt" || f.Name == "Base" || f.Name == "Objects" {
		return entityField{}, fmt.Errorf("vuka add entity: %s: every entity has %s already", arg, f.Name)
	}
	var attrs []string
	unique, index := false, false
	for _, m := range mods {
		switch m {
		case "unique":
			unique = true
		case "index":
			index = true
		default:
			return entityField{}, fmt.Errorf("vuka add entity: %s: %q isn't a field option: unique or index%s", arg, m, suggest(m, []string{"unique", "index"}, ""))
		}
	}
	flags := func() string {
		var fs []string
		if unique {
			fs = append(fs, "Unique: true")
		}
		if index {
			fs = append(fs, "Index: true")
		}
		return strings.Join(fs, ", ")
	}
	plainFlags := func() {
		if unique {
			attrs = append(attrs, "@orm.Unique")
		}
		if index {
			attrs = append(attrs, "@orm.Index")
		}
	}
	join := func(a, b string) string {
		if b == "" {
			return a
		}
		return a + ", " + b
	}
	switch typ {
	case "string":
		f.Type, f.Sample = "string", strconv.Quote("a "+strings.ReplaceAll(name, "_", " "))
		attrs = append(attrs, "@orm.Char{"+join("Max: 255", flags())+"}")
	case "text":
		f.Type, f.Sample = "string", strconv.Quote("Some "+strings.ReplaceAll(name, "_", " ")+".")
		attrs = append(attrs, "@orm.LongText")
		plainFlags()
	case "int", "bigint":
		f.Type, f.Sample = "int64", "3"
		plainFlags()
	case "float":
		f.Type, f.Sample = "float64", "1.5"
		plainFlags()
	case "decimal":
		f.Type, f.Sample = "float64", "9.99"
		attrs = append(attrs, `@orm.DBType("NUMERIC(12,2)")`)
		plainFlags()
	case "bool":
		f.Type, f.Sample = "bool", "true"
		if !null {
			attrs = append(attrs, "@orm.DefaultTo(false)")
		}
		plainFlags()
	case "time":
		f.Type, f.Time, f.Sample = "time.Time", true, "time.Date(2026, 1, 2, 15, 4, 0, 0, time.UTC)"
		if fl := flags(); fl != "" {
			attrs = append(attrs, "@orm.Datetime{"+fl+"}")
		}
	case "date":
		f.Type, f.Time, f.Sample = "time.Time", true, "time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)"
		attrs = append(attrs, "@orm.DateField{"+flags()+"}")
	case "json":
		f.Type, f.Sample = "orm.JSON[map[string]any]", `orm.JSONOf(map[string]any{"a": 1.0})`
		if unique || index {
			return entityField{}, fmt.Errorf("vuka add entity: %s: a json field is neither unique nor indexed", arg)
		}
		null = false
		f.Null = false
	case "ref":
		model, imp, err := findModel(root, mod, pkgDir, camel(name))
		if err != nil {
			return entityField{}, fmt.Errorf("vuka add entity: %s: %v", arg, err)
		}
		f.Ref, f.RefImport = model, imp
		f.Type = "orm.FK[" + model + "]"
		rel := "OnDelete: orm.Cascade"
		if null {
			rel = "OnDelete: orm.SetNull, Null: true"
		}
		attrs = append(attrs, "@orm.Rel{"+rel+"}")
		if unique {
			attrs = append(attrs, "@orm.Unique")
		}
		f.JSON = lowerFirst(f.Name) + "Id"
		f.Column = strings.TrimSuffix(f.Column, "_id") + "_id"
		return f.withAttrs(attrs), nil
	default:
		return entityField{}, fmt.Errorf("vuka add entity: %s: no type %q%s; the types are %s, with ? for an optional one (published_at:time?)",
			arg, typ, suggest(typ, entityTypes, ""), strings.Join(entityTypes, ", "))
	}
	if null {
		f.Type = "Option[" + f.Type + "]"
		if f.Sample != "" {
			f.Sample = "Some(" + f.Sample + ")"
		}
	}
	return f.withAttrs(attrs), nil
}

func (f entityField) withAttrs(attrs []string) entityField {
	f.Attrs = strings.Join(attrs, " ")
	return f
}

var modelRe = regexp.MustCompile(`(?m)^type\s+([A-Z]\w*)\s+struct\s*\{\s*\n\s*orm\.Base\b`)

// findModel is the model a reference names: Name in this package, else in
// another module's (qualified, with its import).
func findModel(root, mod, pkgDir, name string) (string, string, error) {
	models := func(dir string) []string {
		var out []string
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".vuka") || strings.HasSuffix(e.Name(), "_test.vuka") {
				continue
			}
			src, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			for _, m := range modelRe.FindAllSubmatch(src, -1) {
				out = append(out, string(m[1]))
			}
		}
		return out
	}
	own := models(filepath.Join(root, filepath.FromSlash(pkgDir)))
	if slices.Contains(own, name) {
		return name, "", nil
	}
	var found []string
	all := own
	for _, m := range moduleNames(root) {
		if "modules/"+m == pkgDir {
			continue
		}
		ms := models(filepath.Join(root, "modules", m))
		all = append(all, ms...)
		if slices.Contains(ms, name) {
			found = append(found, m)
		}
	}
	switch len(found) {
	case 0:
		return "", "", fmt.Errorf("no model %s in the app%s; add it first (vuka add entity <module>/%s)", name, suggest(name, all, ""), name)
	case 1:
		return found[0] + "." + name, mod + "/modules/" + found[0], nil
	}
	return "", "", fmt.Errorf("%s is a model of modules %s: rename the field after the one you mean", name, strings.Join(found, " and "))
}

func addEntity(root, mod string, f *addFlags, w io.Writer) error {
	if len(f.pos) < 1 {
		return errors.New("usage: vuka add entity <Name> [--module m] [field:type[?][:unique|:index]…] [--table name] [--force] [--dry-run]")
	}
	a := newAdder("entity", root, mod, f)
	module, name, err := moduleAndName("entity", f.pos[0], f)
	if err != nil {
		return err
	}
	if err := checkTypeName("entity", name, "entity"); err != nil {
		return err
	}
	unmanaged := f.bools["unmanaged"]
	if !unmanaged && !exists(root, "migrations") {
		return errors.New("vuka add entity: the app has no migrations/; --unmanaged maps an existing table without one, or start one with vuka new --db=sqlite")
	}
	t, err := a.moduleTarget(module, "entities")
	if err != nil {
		return err
	}
	if at := declared(filepath.Join(root, t.dir), name); at != "" && !a.force {
		return fmt.Errorf("vuka add entity: %s is declared already in %s/%s", name, t.dir, at)
	}
	spec := entitySpec{Pkg: t.pkg, Name: name, Table: f.values["table"], Testable: true, Unmanaged: unmanaged}
	if spec.Table == "" {
		spec.Table = plural(snake(name))
	} else if !fieldName.MatchString(spec.Table) {
		return fmt.Errorf("vuka add entity: --table %s: a table's name is snake_case", spec.Table)
	}
	seen := map[string]bool{}
	imports := map[string]bool{}
	for _, arg := range f.pos[1:] {
		fl, err := parseEntityField(root, mod, t.dir, arg)
		if err != nil {
			return err
		}
		if seen[fl.Name] {
			return fmt.Errorf("vuka add entity: two fields are %s", fl.Name)
		}
		seen[fl.Name] = true
		if fl.Time {
			spec.Time, spec.SampleTime = true, true
		}
		if fl.RefImport != "" {
			imports[fl.RefImport] = true
		}
		if fl.Ref != "" {
			m := fl.Ref
			if !slices.Contains(spec.Refs, m) {
				spec.Refs = append(spec.Refs, m)
			}
			if !fl.Null {
				spec.Testable = false
			}
		}
		spec.Fields = append(spec.Fields, fl)
	}
	spec.Imports = sortedKeys(imports)
	spec.SampleTime = spec.SampleTime && spec.Testable
	if exists(root, filepath.FromSlash(t.dir), snake(name)+".vuka") && !a.force {
		return fmt.Errorf("vuka add entity: %s/%s.vuka exists already (--force overwrites it)", t.dir, snake(name))
	}
	files := [][2]string{{"entity.vuka", snake(name) + ".vuka"}, {"entity_test.vuka", snake(name) + "_test.vuka"}}
	if unmanaged {
		files = files[:1]
	}
	for _, fl := range files {
		src, err := render(fl[0], spec)
		if err != nil {
			return err
		}
		if err := a.create(t.dir+"/"+fl[1], src); err != nil {
			return err
		}
	}
	if err := a.register(t.dir); err != nil {
		return err
	}
	if err := a.commit(w); err != nil {
		return err
	}
	if unmanaged {
		fmt.Fprintf(w, "\n%s maps the existing table %s: no migration. Match its fields to the table's columns (@orm.Column(\"name\") where they differ).\n", name, spec.Table)
		return nil
	}
	if a.dryRun {
		fmt.Fprintf(w, "would write migrations/NNNN_%s.go (go tool orm makemigrations %s)\n", spec.Table, spec.Table)
		return nil
	}
	return makeMigration(root, spec.Table, w)
}

// makeMigration writes the migration of what the models changed, with the
// app's orm tool.
func makeMigration(root, name string, w io.Writer) error {
	gomod, _ := os.ReadFile(filepath.Join(root, "go.mod"))
	if !bytes.Contains(gomod, []byte("tool github.com/vuka-lang/orm/v2/cmd/orm")) {
		fmt.Fprintf(w, "\nWrite its migration: go run github.com/vuka-lang/orm/v2/cmd/orm makemigrations %s\n", name)
		return nil
	}
	c := exec.Command("go", "tool", "orm", "makemigrations", "--noinput", name)
	c.Dir = root
	var out bytes.Buffer
	c.Stdout, c.Stderr = &out, &out
	step := task(w, "writing the migration (go tool orm makemigrations)", "migration written", "go tool orm makemigrations failed", "still building the app").Start()
	err := c.Run()
	step.End(err, lastLines(out.String(), 30))
	if err != nil {
		return fmt.Errorf("vuka add entity: the model is written, its migration isn't: fix the error, then go tool orm makemigrations %s", name)
	}
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if l = strings.TrimSpace(strings.ReplaceAll(l, root+string(filepath.Separator), "")); l != "" {
			fmt.Fprintln(w, "  "+l)
		}
	}
	fmt.Fprintln(w, "\nThe app applies the migration when it next starts.")
	return nil
}
