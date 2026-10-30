package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// vuka ui: components written into the app as its own source, drawn with
// Tailwind utilities over design tokens in its stylesheet, from a registry
// (uiregistry.go).

const uiUsage = `Usage:

	vuka ui init [-f] [--dry-run] [--css file] [--dir dir] [--package name]
	vuka ui add [-f] [--dry-run] <component…>
	vuka ui diff [component…]
	vuka ui list

Components that are the app's own source: vuka ui add writes each one into
the app, a .vuka.html file you change as you like, drawn with Tailwind
utilities over design tokens in the app's stylesheet.

  init   writes the design tokens into the app's stylesheet (app.css or
         styles/app.css, else the one web.Tailwind names; --css another),
         components/ui/cn.go (--dir another directory; --package names
         its package, design by default), and a [ui] table in vuka.toml
         recording them. What it finds there already it leaves as it is:
         -f writes the tokens again.
  add    writes the components named, and those they use, into the
         components directory. A file the app has already is left as it
         is: when it differs, add prints the difference and stops, unless
         -f, which overwrites it. A component needing newer vuka libraries
         than the app's go.mod requires says which go get to run.
  diff   prints how the app's copies of the components named (all it has
         when none are) differ from the registry's; vuka ui add -f takes
         the registry's.
  list   lists the components.

The registry is vuka's own, fetched at the version of this vuka, or the
copy built into it with --offline or when the fetch fails. --registry
(or registry in vuka.toml's [ui]) names another: an https:// URL or a
directory holding an index.json.

--dry-run says what init or add would write, writing nothing.
`

// uiCmd is vuka ui.
func uiCmd(args []string, stdout io.Writer) error {
	if len(args) == 0 || slices.Contains([]string{"-h", "-help", "--help", "help"}, args[0]) {
		fmt.Fprint(stdout, uiUsage)
		return nil
	}
	sub := args[0]
	f, err := parseUIFlags(sub, args[1:])
	if err != nil {
		return err
	}
	if !slices.Contains([]string{"init", "add", "diff", "list"}, sub) {
		return fmt.Errorf("vuka ui: no command %q%s; the commands are init, add, diff and list", sub, suggest(sub, []string{"init", "add", "diff", "list"}, ""))
	}
	root, modPath, err := load.ModuleRoot(".")
	if err != nil && sub != "list" {
		return err
	}
	var cfg uiConfig
	if root != "" {
		if cfg, _, err = readUIConfig(root); err != nil {
			return err
		}
	}
	switch sub {
	case "list":
		if len(f.pos) > 0 {
			return errors.New("usage: vuka ui list")
		}
		reg, err := openRegistry(f, cfg, root)
		if err != nil {
			return err
		}
		reg.list(stdout)
		return nil
	case "init":
		if len(f.pos) > 0 {
			return fmt.Errorf("vuka ui init takes no arguments (vuka ui add %s adds components)", strings.Join(f.pos, " "))
		}
		return uiInit(root, modPath, f, stdout)
	case "diff":
		return uiDiff(root, f, stdout)
	}
	if len(f.pos) == 0 {
		return errors.New("usage: vuka ui add [-f] [--dry-run] <component…> (vuka ui list lists them)")
	}
	return uiAdd(root, modPath, f, stdout)
}

// uiFlags are a vuka ui command's arguments.
type uiFlags struct {
	pos             []string
	force, dryRun   bool
	offline         bool
	css, dir, pkg   string
	style, registry string
	setDir, setPkg  bool
}

func parseUIFlags(sub string, args []string) (*uiFlags, error) {
	f := &uiFlags{}
	values := map[string]*string{"registry": &f.registry}
	bools := map[string]*bool{"offline": &f.offline}
	switch sub {
	case "init":
		values["css"], values["dir"], values["package"], values["style"] = &f.css, &f.dir, &f.pkg, &f.style
		bools["f"], bools["force"], bools["dry-run"] = &f.force, &f.force, &f.dryRun
	case "add":
		bools["f"], bools["force"], bools["dry-run"] = &f.force, &f.force, &f.dryRun
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" {
			f.pos = append(f.pos, a)
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(a, "-"), "=")
		switch {
		case bools[name] != nil && !hasValue:
			*bools[name] = true
		case values[name] != nil:
			if !hasValue {
				if i+1 >= len(args) {
					return nil, fmt.Errorf("vuka ui %s: --%s needs a value", sub, name)
				}
				i++
				value = args[i]
			}
			*values[name] = value
			f.setDir = f.setDir || name == "dir"
			f.setPkg = f.setPkg || name == "package"
		default:
			var names []string
			for n := range bools {
				if n != "f" {
					names = append(names, n)
				}
			}
			for n := range values {
				names = append(names, n)
			}
			slices.Sort(names)
			return nil, fmt.Errorf("vuka ui %s: no flag %s%s", sub, a, suggest(name, names, "--"))
		}
	}
	return f, nil
}

// uiConfig is vuka.toml's [ui] table: where vuka ui writes.
type uiConfig struct {
	Style   string // the tokens' style
	Dir     string // the components' directory, relative to the module
	Package string // their package's name
	CSS     string // the stylesheet holding the tokens, relative to the module
	// Registry is the registry vuka ui reads: an https:// URL, or a
	// directory relative to the module; vuka's own when empty.
	Registry string
}

var uiKeys = []string{"style", "components", "package", "css", "registry"}

// readUIConfig reads the [ui] table of the vuka.toml at root; ok is false
// when there is none.
func readUIConfig(root string) (c uiConfig, ok bool, err error) {
	file := filepath.Join(root, "vuka.toml")
	src, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return c, false, nil
	} else if err != nil {
		return c, false, err
	}
	table := ""
	for n, line := range strings.Split(string(src), "\n") {
		l := strings.TrimSpace(stripComment(line))
		switch {
		case l == "":
			continue
		case strings.HasPrefix(l, "["):
			table = strings.TrimSpace(strings.Trim(l, "[]"))
			ok = ok || table == "ui"
			continue
		case table != "ui":
			continue
		}
		at := fmt.Sprintf("vuka.toml:%d", n+1)
		key, value, found := strings.Cut(l, "=")
		key = strings.TrimSpace(key)
		s, qerr := strconv.Unquote(strings.TrimSpace(value))
		if !found || qerr != nil {
			return c, ok, fmt.Errorf("%s: [ui] has key = \"value\" lines", at)
		}
		switch key {
		case "style":
			c.Style = s
		case "components":
			c.Dir = s
		case "package":
			c.Package = s
		case "css":
			c.CSS = s
		case "registry":
			c.Registry = s
		default:
			return c, ok, fmt.Errorf("%s: unknown key %s in [ui]%s; its keys are %s", at, key, suggest(key, uiKeys, ""), strings.Join(uiKeys, ", "))
		}
	}
	if ok && (c.Dir == "" || c.Package == "") {
		return c, ok, errors.New("vuka.toml: [ui] names the components' directory (components) and package (package)")
	}
	return c, ok, nil
}

// stripComment is a TOML line without its # comment.
func stripComment(l string) string {
	in := false
	for i := 0; i < len(l); i++ {
		switch l[i] {
		case '"':
			in = !in
		case '#':
			if !in {
				return l[:i]
			}
		}
	}
	return l
}

// table is the [ui] table c is.
func (c uiConfig) table() string {
	t := fmt.Sprintf("[ui]\nstyle = %q\ncomponents = %q\npackage = %q\ncss = %q\n", c.Style, c.Dir, c.Package, c.CSS)
	if c.Registry != "" {
		t += fmt.Sprintf("registry = %q\n", c.Registry)
	}
	return t
}

var goPackageName = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// tokensStart and tokensEnd mark the block of design tokens vuka ui init
// writes into the stylesheet.
const (
	tokensStart = "/* vuka ui: the design tokens"
	tokensEnd   = "/* vuka ui: end of the design tokens */"
)

var (
	ownTokens  = regexp.MustCompile(`--color-background\s*:`)
	cssImports = regexp.MustCompile(`\A(?:[ \t]*(?:@import[^\n]*|@charset[^\n]*|)\n)*`)
)

// uiInit is vuka ui init.
func uiInit(root, modPath string, f *uiFlags, w io.Writer) error {
	cfg, had, err := readUIConfig(root)
	if err != nil {
		return err
	}
	if f.registry != "" {
		cfg.Registry = f.registry
		if src, err := parseRegistry(f.registry, ""); err == nil && src.dir != "" {
			if rel, err := filepath.Rel(root, src.dir); err == nil {
				cfg.Registry = filepath.ToSlash(rel)
			}
		}
	}
	reg, err := openRegistry(f, cfg, root)
	if err != nil {
		return err
	}
	if !had {
		cfg = uiConfig{Style: "default", Dir: "components/ui", Package: reg.Package, Registry: cfg.Registry}
	}
	if f.style != "" {
		cfg.Style = f.style
	}
	if f.setDir {
		cfg.Dir = filepath.ToSlash(filepath.Clean(f.dir))
	}
	if f.setPkg {
		cfg.Package = f.pkg
	}
	if f.css != "" {
		cfg.CSS = filepath.ToSlash(filepath.Clean(f.css))
	}
	if _, ok := reg.Styles[cfg.Style]; !ok {
		return fmt.Errorf("vuka ui init: no style %q; the styles are %s", cfg.Style, strings.Join(slices.Sorted(mapKeys(reg.Styles)), ", "))
	}
	if !goPackageName.MatchString(cfg.Package) || cfg.Package == "ui" {
		return fmt.Errorf("vuka ui init: --package %s: a package name in lower case, other than ui (the components use github.com/vuka-lang/ui/v2's ui)", cfg.Package)
	}
	if strings.HasPrefix(cfg.Dir, "../") || filepath.IsAbs(cfg.Dir) {
		return fmt.Errorf("vuka ui init: --dir %s: a directory inside the module", cfg.Dir)
	}
	if cfg.CSS == "" {
		cfg.CSS = stylesheet(root)
	}
	cn, err := reg.resolve("add", []string{"cn"})
	if err != nil {
		return err
	}
	if _, err := reg.fetch(cn, cfg.Style); err != nil {
		return err
	}
	out := &uiWriter{root: root, dryRun: f.dryRun}
	if reg.note != "" {
		out.note("%s", reg.note)
	}

	// The tokens.
	tokens, err := reg.file(reg.Styles[cfg.Style])
	if err != nil {
		return err
	}
	cssFile := filepath.Join(root, filepath.FromSlash(cfg.CSS))
	css, err := os.ReadFile(cssFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	switch start, end := bytes.Index(css, []byte(tokensStart)), bytes.Index(css, []byte(tokensEnd)); {
	case start >= 0 && end > start && !f.force:
		out.note("%s has the design tokens (-f writes them again)", cfg.CSS)
	case start >= 0 && end > start:
		end += len(tokensEnd)
		if end < len(css) && css[end] == '\n' {
			end++
		}
		out.write(cfg.CSS, slices.Concat(css[:start], tokens, css[end:]), "the design tokens")
	case ownTokens.Match(css) && !f.force:
		out.note("%s has design tokens of its own (--color-background); -f adds vuka ui's after its imports", cfg.CSS)
	default:
		if !twImport.Match(css) {
			css = append([]byte("@import \"tailwindcss\";\n\n"), css...)
		}
		at := len(cssImports.Find(css))
		out.write(cfg.CSS, slices.Concat(css[:at], sep(css[:at]), tokens, sep2(css[at:]), css[at:]), "the design tokens")
	}

	// cn.go.
	if _, err := addItems(out, reg, cfg, []string{"cn"}, f.force, false); err != nil {
		return err
	}

	// vuka.toml.
	tomlFile := filepath.Join(root, "vuka.toml")
	toml, err := os.ReadFile(tomlFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	switch next := []byte(cfg.table()); {
	case !had:
		if len(toml) > 0 && !bytes.HasSuffix(toml, []byte("\n")) {
			toml = append(toml, '\n')
		}
		if len(toml) > 0 {
			toml = append(toml, '\n')
		}
		out.write("vuka.toml", append(toml, next...), "[ui]")
	case bytes.Contains(toml, next):
		out.note("vuka.toml has [ui]")
	default:
		out.write("vuka.toml", replaceUITable(toml, next), "[ui]")
	}
	if err := out.commit(w); err != nil {
		return err
	}
	if !f.dryRun && len(out.files) > 0 {
		fmt.Fprintf(w, "\nAdd components with vuka ui add (vuka ui list lists them), then use them as tags:\n\n\timport %s %q\n\n\t<%s.Button>Save</%s.Button>\n",
			cfg.Package, modPath+"/"+cfg.Dir, cfg.Package, cfg.Package)
	}
	return nil
}

// stylesheet is the app's Tailwind stylesheet, relative to root: the one
// vuka builds (app.css, styles/app.css, or web.Tailwind's), else
// styles/app.css, to make.
func stylesheet(root string) string {
	if tw, err := tailwindFor(root); err == nil && tw != nil && tw.sheet != "" {
		if rel, err := filepath.Rel(root, tw.sheet); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	for _, c := range []string{"app.css", "styles/app.css"} {
		if exists(root, filepath.FromSlash(c)) {
			return c
		}
	}
	return "styles/app.css"
}

// sep is the blank line between what comes before the tokens and them.
func sep(before []byte) []byte {
	switch {
	case len(before) == 0:
		return nil
	case bytes.HasSuffix(before, []byte("\n\n")):
		return nil
	}
	return []byte("\n")
}

// sep2 is the blank line between the tokens and what comes after them.
func sep2(after []byte) []byte {
	if len(after) == 0 || bytes.HasPrefix(after, []byte("\n")) {
		return nil
	}
	return []byte("\n")
}

// replaceUITable is toml with its [ui] table replaced by table.
func replaceUITable(toml, table []byte) []byte {
	lines := strings.SplitAfter(string(toml), "\n")
	var out strings.Builder
	in := false
	for _, l := range lines {
		t := strings.TrimSpace(stripComment(l))
		if strings.HasPrefix(t, "[") {
			in = strings.TrimSpace(strings.Trim(t, "[]")) == "ui"
			if in {
				out.Write(table)
				continue
			}
		}
		if !in {
			out.WriteString(l)
		}
	}
	return []byte(out.String())
}

func mapKeys[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// uiAdd is vuka ui add.
func uiAdd(root, modPath string, f *uiFlags, w io.Writer) error {
	cfg, ok, err := readUIConfig(root)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("vuka ui add: vuka.toml has no [ui]: run vuka ui init first, which writes the design tokens the components are drawn with")
	}
	reg, err := openRegistry(f, cfg, root)
	if err != nil {
		return err
	}
	items, err := reg.resolve("add", f.pos)
	if err != nil {
		return err
	}
	if _, err := reg.fetch(items); err != nil {
		return err
	}
	out := &uiWriter{root: root, dryRun: f.dryRun}
	if reg.note != "" {
		out.note("%s", reg.note)
	}
	writing, err := addItems(out, reg, cfg, f.pos, f.force, true)
	if err != nil {
		return err
	}
	if err := checkRequires(root, writing); err != nil {
		return err
	}
	if err := out.commit(w); err != nil {
		return err
	}
	switch {
	case len(out.differ) == 0 || f.dryRun:
	case len(out.differ) == 1:
		return fmt.Errorf("vuka ui add: %s differs from the registry's (the difference is above); -f overwrites it", out.differ[0])
	default:
		return fmt.Errorf("vuka ui add: %s differ from the registry's (the differences are above); -f overwrites them", strings.Join(out.differ, ", "))
	}
	if !f.dryRun && len(out.files) > 0 {
		fmt.Fprintf(w, "\nUse them as tags (import %s %q):\n\n", cfg.Package, modPath+"/"+cfg.Dir)
		for _, n := range f.pos {
			if ex := uiExample[strings.ToLower(n)]; ex != "" {
				fmt.Fprintf(w, "\t%s\n", strings.ReplaceAll(ex, "design.", cfg.Package+"."))
			}
		}
	}
	return nil
}

// uiExample is a line using each component, printed once it's added.
var uiExample = map[string]string{
	"button":   `<design.Button variant={design.ButtonOutline} on={p.Save}>Save</design.Button>`,
	"badge":    `<design.Badge variant={design.BadgeSecondary}>Draft</design.Badge>`,
	"card":     `<design.Card title="Team" description="Who can see this.">…</design.Card>`,
	"alert":    `<design.Alert title="Heads up">…</design.Alert>`,
	"input":    `<design.Input id="email" name="email" type="email" />`,
	"label":    `<design.Label for="email">Email</design.Label>`,
	"tabs":     `<design.Tabs default="a"><slot:tab value="a" label="A">…</slot:tab></design.Tabs>`,
	"dialog":   `<design.Button on={design.OpenDialog("confirm")}>Open</design.Button> <design.Dialog id="confirm" title="Sure?">…</design.Dialog>`,
	"skeleton": `<design.Skeleton lines={3}/> <design.SkeletonOf repeat={3}><PostRow post={placeholder}/></design.SkeletonOf>`,
}

// addItems queues the files of the items names asks for, and of those they
// use, on out. A file the app has that differs from the registry's is a
// difference printed for an item asked for when explicit (refused unless
// force, which overwrites it), and kept as the app's otherwise.
func addItems(out *uiWriter, reg *registry, cfg uiConfig, names []string, force, explicit bool) (writing []*registryItem, err error) {
	items, err := reg.resolve("add", names)
	if err != nil {
		return nil, err
	}
	asked := map[string]bool{}
	for _, n := range names {
		asked[strings.ToLower(n)] = explicit
	}
	for _, it := range items {
		writes := false
		for _, file := range it.Files {
			src, rel, have, err := appFile(out.root, reg, cfg, file)
			switch {
			case err != nil:
				return nil, err
			case have == nil:
				out.write(rel, src, "")
				writes = true
			case bytes.Equal(have, src):
				if asked[it.Name] {
					out.note("%s is up to date", rel)
				}
			case asked[it.Name] && force:
				out.write(rel, src, "")
				writes = true
			case asked[it.Name]:
				out.differs(rel, have, src)
			default:
				out.note("%s is yours, differing from the registry's (vuka ui diff %s shows how)", rel, it.Name)
			}
		}
		if writes {
			writing = append(writing, it)
		}
	}
	return writing, nil
}

// appFile is a registry file as the app gets it (its package clause the
// app's), where it goes, and the app's copy (nil when it has none).
func appFile(root string, reg *registry, cfg uiConfig, file registryFile) (src []byte, rel string, have []byte, err error) {
	src, err = reg.file(file)
	if err != nil {
		return nil, "", nil, err
	}
	src = withPackage(src, reg.Package, cfg.Package)
	_, name, _ := strings.Cut(file.Path, "/")
	rel = path.Join(cfg.Dir, name)
	have, err = os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return src, rel, nil, nil
	case err != nil:
		return nil, "", nil, err
	case have == nil:
		have = []byte{}
	}
	return src, rel, have, nil
}

// uiDiff is vuka ui diff.
func uiDiff(root string, f *uiFlags, w io.Writer) error {
	cfg, ok, err := readUIConfig(root)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("vuka ui diff: vuka.toml has no [ui]: run vuka ui init first")
	}
	reg, err := openRegistry(f, cfg, root)
	if err != nil {
		return err
	}
	var items []*registryItem
	for _, n := range f.pos {
		it := reg.item(strings.ToLower(n))
		if it == nil {
			return fmt.Errorf("vuka ui diff: no component %q%s; the components are %s", n,
				suggest(n, reg.components(), ""), strings.Join(reg.components(), ", "))
		}
		items = append(items, it)
	}
	if len(f.pos) == 0 {
		for i := range reg.Items {
			it := &reg.Items[i]
			for _, file := range it.Files {
				_, name, _ := strings.Cut(file.Path, "/")
				if exists(root, filepath.FromSlash(path.Join(cfg.Dir, name))) {
					items = append(items, it)
					break
				}
			}
		}
	}
	if items, err = reg.fetch(items); err != nil {
		return err
	}
	if reg.note != "" {
		fmt.Fprintln(w, reg.note)
	}
	var diffs bytes.Buffer
	var differ, same []string
	for _, it := range items {
		changed := false
		for _, file := range it.Files {
			src, rel, have, err := appFile(root, reg, cfg, file)
			switch {
			case err != nil:
				return err
			case have == nil:
				fmt.Fprintf(w, "%s: the app hasn't %s\n", it.Name, rel)
				changed = true
			case !bytes.Equal(have, src):
				diffs.Write(unifiedText(rel, have, src))
				changed = true
			}
		}
		if changed {
			differ = append(differ, it.Name)
		} else {
			same = append(same, it.Name)
		}
	}
	w.Write(diffs.Bytes())
	switch {
	case len(items) == 0:
		fmt.Fprintf(w, "%s has none of the registry's components\n", cfg.Dir)
	case len(differ) == 0:
		fmt.Fprintf(w, "%s: the same as the registry's\n", strings.Join(same, ", "))
	default:
		if diffs.Len() > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "vuka ui add -f %s takes the registry's", strings.Join(differ, " "))
		if len(same) > 0 {
			fmt.Fprintf(w, "; %s: the same as the registry's", strings.Join(same, ", "))
		}
		fmt.Fprintln(w)
	}
	return nil
}

// withPackage is a registry file with its package clause naming pkg.
func withPackage(src []byte, from, pkg string) []byte {
	if from == pkg {
		return src
	}
	re := regexp.MustCompile(`(?m)^package ` + regexp.QuoteMeta(from) + `$`)
	if loc := re.FindIndex(src); loc != nil {
		return slices.Concat(src[:loc[0]], []byte("package "+pkg), src[loc[1]:])
	}
	return src
}

// uiWriter collects what a vuka ui command writes, and says so.
type uiWriter struct {
	root   string
	dryRun bool
	files  []string
	data   map[string][]byte
	what   map[string]string
	lines  []string // what it says, in order
	diffs  []byte
	differ []string
}

func (u *uiWriter) write(rel string, b []byte, what string) {
	if u.data == nil {
		u.data, u.what = map[string][]byte{}, map[string]string{}
	}
	if _, ok := u.data[rel]; !ok {
		u.files = append(u.files, rel)
	}
	u.data[rel], u.what[rel] = b, what
	verb := "wrote"
	if u.dryRun {
		verb = "would write"
	}
	if what != "" {
		u.lines = append(u.lines, fmt.Sprintf("%s %s: %s", verb, rel, what))
	} else {
		u.lines = append(u.lines, fmt.Sprintf("%s %s", verb, rel))
	}
}

func (u *uiWriter) note(format string, args ...any) {
	u.lines = append(u.lines, fmt.Sprintf(format, args...))
}

func (u *uiWriter) differs(rel string, have, want []byte) {
	u.differ = append(u.differ, rel)
	u.lines = append(u.lines, rel+" differs from the registry's:")
	u.diffs = append(u.diffs, unifiedText(rel, have, want)...)
}

// commit writes the files (none with --dry-run) and reports.
func (u *uiWriter) commit(w io.Writer) error {
	if !u.dryRun {
		for _, rel := range u.files {
			p := filepath.Join(u.root, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(p, u.data[rel], 0o644); err != nil {
				return err
			}
		}
	}
	for _, l := range u.lines {
		fmt.Fprintln(w, l)
	}
	if len(u.diffs) > 0 {
		fmt.Fprintf(w, "\n%s", u.diffs)
	}
	return nil
}

// unifiedText is a unified diff from a to b, both named rel: the app's
// file (---) and the registry's (+++), three lines of context.
func unifiedText(rel string, a, b []byte) []byte {
	al, bl := splitLines(a), splitLines(b)
	ids := map[string]int{}
	intern := func(lines []string) []int {
		out := make([]int, len(lines))
		for i, l := range lines {
			id, ok := ids[l]
			if !ok {
				id = len(ids)
				ids[l] = id
			}
			out[i] = id
		}
		return out
	}
	pairs, _ := myers(intern(al), intern(bl), len(al)+len(bl))
	// ops: ' ', '-' or '+' and the line, in order.
	type op struct {
		kind byte
		line string
		ai   int // the line's number in a (for ' ' and '-'), else the next one's
		bi   int
	}
	var ops []op
	x, y := 0, 0
	for _, p := range append(pairs, [2]int{len(al), len(bl)}) {
		for ; x < p[0]; x++ {
			ops = append(ops, op{'-', al[x], x, y})
		}
		for ; y < p[1]; y++ {
			ops = append(ops, op{'+', bl[y], x, y})
		}
		if x < len(al) && y < len(bl) {
			ops = append(ops, op{' ', al[x], x, y})
			x, y = x+1, y+1
		}
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "--- %s\n+++ %s (vuka ui add)\n", rel, rel)
	const context = 3
	for i := 0; i < len(ops); {
		if ops[i].kind == ' ' {
			i++
			continue
		}
		start := max(0, i-context)
		end := i
		for end < len(ops) {
			if ops[end].kind != ' ' {
				end++
				continue
			}
			run := end
			for run < len(ops) && ops[run].kind == ' ' {
				run++
			}
			if run == len(ops) || run-end > 2*context {
				end = min(len(ops), end+context)
				break
			}
			end = run
		}
		var an, bn int
		for _, o := range ops[start:end] {
			if o.kind != '+' {
				an++
			}
			if o.kind != '-' {
				bn++
			}
		}
		fmt.Fprintf(&buf, "@@ -%s +%s @@\n", hunkRange(ops[start].ai, an), hunkRange(ops[start].bi, bn))
		for _, o := range ops[start:end] {
			buf.WriteByte(o.kind)
			buf.WriteString(o.line)
			if !strings.HasSuffix(o.line, "\n") {
				buf.WriteString("\n\\ No newline at end of file\n")
			}
		}
		i = end
	}
	return buf.Bytes()
}

func hunkRange(start, n int) string {
	if n == 0 {
		return fmt.Sprintf("%d,0", start)
	}
	if n == 1 {
		return strconv.Itoa(start + 1)
	}
	return fmt.Sprintf("%d,%d", start+1, n)
}

func splitLines(b []byte) []string {
	var out []string
	for len(b) > 0 {
		i := bytes.IndexByte(b, '\n') + 1
		if i == 0 {
			i = len(b)
		}
		out = append(out, string(b[:i]))
		b = b[i:]
	}
	return out
}
