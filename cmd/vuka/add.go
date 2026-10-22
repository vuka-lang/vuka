package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

const addUsage = `Usage:

	vuka add module <name>
	vuka add service <Name> [--module m]
	vuka add component <Name> [--module m]
	vuka add island <Name> [--module m] [--vue|--react|--svelte] [--dir dir] [--no-install]
	vuka add page <path> [--live|--stateless] [--island Name [--module m] [--vue|--react|--svelte]] [--name Type]
	vuka add entity <Name> [--module m] [field:type…] [--table name] [--unmanaged]

Grow an app vuka new made.

  module     modules/<name>/: a model, a service, a test and a migration
             making its table, with main.vuka's import of it; and its page,
             pages/<name>/index.vuka.html, at /<name> (templ views: handlers
             and a view in the module; an API: its routes in the module).
             The name is a plural noun in lower case: notes, invoices.
  service    a service in services/ (or the module's package; m/Name is
             short for Name --module m), with its constructor NewName and a
             test, registered in main.vuka's services with app.Service: a
             dependency pages, routes and hooks take as a parameter.
  component  a component, one .vuka.html file: its struct (the props, its
             exported fields) above its markup, in components/ (or the
             module's package).
  island     a client component in the island frontend (web/islands/ by
             default, web/islands/<m>/ with --module m), in the framework the
             frontend uses (Vue when it has none; --react, --svelte or
             --vue to add one in another), its props typed
             so an import reads them; and the .vuka.html component placing
             it, in components/ (or the module's package). The framework's
             packages and Vite plugin are added when missing, and installed
             (--no-install: not).
  page       a page: the .vuka.html file its path names under pages/
             (/reports/{id} is pages/reports/[id].vuka.html, a parameter a
             field of its struct), live (--stateless: built per request; a
             path with {parameters} is stateless unless --live). --island
             Name makes it a page backed by a component: pages/<path>.vuka,
             an @ui.Island struct with a prop, a url: field and a method, and
             the component calling it (ui.Live unless --stateless; --module m
             puts the component in web/islands/<m>/). The folder's
             _layout.vuka.html is its layout.
  entity     an orm model in entities/ (or the module's package; m/Name is
             short for Name --module m), with ID and created/updated times,
             its fields name:type — string, text, int, float, decimal,
             bool, time, date, json, ref (a foreign key to the model the
             name names: user:ref) — ? for an optional one (published_at:time?),
             :unique or :index after (email:string:unique); a test, and its
             migration from go tool orm makemigrations; --unmanaged maps an
             existing table instead (no timestamps, test or migration).

Every kind: --dry-run lists what it would write, --force overwrites what
exists (nothing is overwritten otherwise). Files are formatted as vuka fmt
formats them, and a new package is imported in main.vuka.
`

// add is vuka add: it grows an app, a piece at a time.
func add(args []string, stdout io.Writer) error {
	if len(args) == 0 || len(args) == 1 && slices.Contains([]string{"-h", "-help", "--help", "help"}, args[0]) {
		fmt.Fprint(stdout, addUsage)
		return nil
	}
	kinds := map[string]func(root, mod string, f *addFlags, w io.Writer) error{
		"service": addService, "component": addComponent, "island": addIsland, "page": addPage, "entity": addEntity,
	}
	kind := args[0]
	if kind != "module" && kinds[kind] == nil {
		return fmt.Errorf("vuka add: no kind %q%s; the kinds are module, service, component, island, page and entity", kind,
			suggest(kind, []string{"module", "service", "component", "island", "page", "entity"}, ""))
	}
	root, modPath, err := load.ModuleRoot(".")
	if err != nil {
		return err
	}
	if kind != "module" {
		f, err := parseAddFlags(kind, args[1:])
		if err != nil {
			return err
		}
		return kinds[kind](root, modPath, f, stdout)
	}
	if len(args) != 2 {
		return errors.New(strings.TrimRight(addUsage, "\n"))
	}
	files, err := addModule(root, modPath, args[1])
	if err != nil {
		return err
	}
	for _, f := range files {
		fmt.Fprintln(stdout, "wrote", f)
	}
	fmt.Fprintf(stdout, "\nThe app applies the migration when it next starts. Then:\n\n\tvuka test ./modules/%s\n\tvuka run -watch .\n", args[1])
	return nil
}

// moduleSpec is what the module templates read.
type moduleSpec struct {
	Module                     string // the app's module path
	Pkg, Model, Service, Label string // notes, Note, Notes, Notes
	Table, Path                string // notes, /notes
	UI                         string // live, templ, api
	Auth                       bool
	Migration, Dependency      string // 0002_notes, 0001_initial
}

var moduleName = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// addModule writes module name into the app at root and registers it; it
// returns the files written, relative to root.
func addModule(root, modPath, name string) ([]string, error) {
	if !moduleName.MatchString(name) || token.IsKeyword(name) {
		return nil, fmt.Errorf("vuka add module %s: a module's name is lower-case letters and digits, a plural noun: notes, invoices", name)
	}
	if name == "migrations" || name == "users" && exists(root, "modules", "users") {
		return nil, fmt.Errorf("vuka add module %s: the app has it already", name)
	}
	if exists(root, "modules", name) {
		return nil, fmt.Errorf("vuka add module %s: modules/%s exists already", name, name)
	}
	if exists(root, "pages", name) && exists(root, "layouts", "app.vuka") {
		return nil, fmt.Errorf("vuka add module %s: pages/%s exists already", name, name)
	}
	if !exists(root, "migrations") {
		return nil, errors.New("vuka add module: the app has no migrations/ (no database): a module keeps its rows in the database; start one with vuka new --db=sqlite")
	}
	mainFile := filepath.Join(root, "main.vuka")
	mainSrc, err := os.ReadFile(mainFile)
	if err != nil {
		return nil, fmt.Errorf("vuka add module: %v (it registers the module in main.vuka)", err)
	}
	spec := moduleSpec{Module: modPath, Pkg: name, Model: singular(name), Service: displayName(name), Label: displayName(name),
		Table: name, Path: "/" + name, Auth: exists(root, "modules", "users")}
	switch {
	case exists(root, "layouts", "app.vuka"):
		spec.UI = "live"
	case exists(root, "layouts", "layouts.templ"):
		spec.UI = "templ"
	default:
		spec.UI = "api"
	}
	if spec.Model == spec.Service {
		spec.Service += "Store" // a name that is its own plural: news
	}
	n, last, err := migrations(filepath.Join(root, "migrations"))
	if err != nil {
		return nil, err
	}
	spec.Migration, spec.Dependency = fmt.Sprintf("%04d_%s", n+1, name), last

	files, err := renderTree("scaffold/module", map[string]bool{spec.UI: true, "pages": spec.UI != "api", "auth": spec.Auth}, spec, modPath)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for f, b := range files {
		switch f {
		case "migration.go":
			out["migrations/"+spec.Migration+".go"] = b
		case "module_test.vuka":
			out["modules/"+name+"/"+name+"_test.vuka"] = b
		case "page.templ":
			out["modules/"+name+"/"+name+".templ"] = b
		case "page.vuka.html":
			out["pages/"+name+"/index.vuka.html"] = b
		default:
			out["modules/"+name+"/"+f] = b
		}
	}
	for f := range out {
		if exists(root, filepath.FromSlash(f)) {
			return nil, fmt.Errorf("vuka add module %s: %s exists already", name, f)
		}
	}
	registered, err := registerImport(mainSrc, modPath, modPath+"/modules/"+name)
	if err != nil {
		return nil, err
	}
	registered, serviced, err := registerService(registered, modPath, modPath+"/modules/"+name, name, "New"+spec.Service)
	if err != nil {
		return nil, err
	}
	if _, err := writeFiles(root, out); err != nil {
		return nil, err
	}
	if err := os.WriteFile(mainFile, registered, 0o644); err != nil {
		return nil, err
	}
	var written []string
	for f := range out {
		written = append(written, f)
	}
	slices.Sort(written)
	if !serviced {
		return append(written, fmt.Sprintf("main.vuka (imports the module; register its service: app.Service(%s.New%s))", name, spec.Service)), nil
	}
	return append(written, "main.vuka (imports the module, registers its service)"), nil
}

func exists(root string, elem ...string) bool {
	_, err := os.Stat(filepath.Join(append([]string{root}, elem...)...))
	return err == nil
}

// singular is a plural noun's singular, as a type name: notes is Note,
// categories Category.
func singular(plural string) string {
	s := plural
	switch {
	case strings.HasSuffix(s, "ies") && len(s) > 3:
		s = s[:len(s)-3] + "y"
	case strings.HasSuffix(s, "sses"), strings.HasSuffix(s, "xes"), strings.HasSuffix(s, "ches"), strings.HasSuffix(s, "shes"):
		s = s[:len(s)-2]
	case strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss"):
		s = s[:len(s)-1]
	}
	return displayName(s)
}

var migrationFile = regexp.MustCompile(`^(\d{4})_(\w+)\.go$`)

// migrations is the highest number among the migration files in dir, and
// that migration's name: what the next one depends on.
func migrations(dir string) (int, string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, "", err
	}
	high, last := 0, ""
	for _, e := range entries {
		m := migrationFile.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		if n, _ := strconv.Atoi(m[1]); n > high {
			high, last = n, strings.TrimSuffix(e.Name(), ".go")
		}
	}
	return high, last, nil
}

// registerService registers ctor, a constructor of the package pkg (named
// name), in main.vuka: the package imported by name and
// app.Service(name.ctor) added to the app's services function. It reports
// false, src unchanged, when main.vuka has no services function, as an app
// made before it may not.
func registerService(src []byte, modPath, pkg, name, ctor string) ([]byte, bool, error) {
	fn := regexp.MustCompile(`(?m)^func services\(app \*web\.App\) \{\n`).FindIndex(src)
	if fn == nil {
		return src, false, nil
	}
	call := "app.Service(" + name + "." + ctor + ")"
	if bytes.Contains(src, []byte(call)) {
		return src, true, nil
	}
	end := bytes.Index(src[fn[1]-1:], []byte("\n}")) + fn[1] - 1
	out := slices.Concat(src[:end], []byte("\n\t"+call), src[end:])
	block := regexp.MustCompile(`(?s)\nimport \(\n(.*?)\n\)`).FindSubmatchIndex(out)
	if block == nil {
		return nil, false, errors.New("main.vuka has no import ( … ) block to import the service's package in")
	}
	imports := string(out[block[2]:block[3]])
	quoted := strconv.Quote(pkg)
	switch {
	case strings.Contains(imports, "_ "+quoted):
		imports = strings.Replace(imports, "_ "+quoted, quoted, 1)
	case !strings.Contains(imports, quoted):
		imports += "\n\t" + quoted
	}
	out = slices.Concat(out[:block[2]], []byte(imports), out[block[3]:])
	b, err := formatSource("main.vuka", out, modPath)
	return b, err == nil, err
}

// registerImport adds a blank import of pkg to main.vuka's import block,
// so the module's routes and pages are declared.
func registerImport(src []byte, modPath, pkg string) ([]byte, error) {
	block := regexp.MustCompile(`(?s)\nimport \(\n(.*?)\n\)`).FindSubmatchIndex(src)
	if block == nil {
		return nil, errors.New("vuka add module: main.vuka has no import ( … ) block to register the module in")
	}
	if strings.Contains(string(src[block[2]:block[3]]), strconv.Quote(pkg)) {
		return src, nil
	}
	line := []byte("\n\t_ " + strconv.Quote(pkg))
	out := slices.Concat(src[:block[3]], line, src[block[3]:])
	return formatSource("main.vuka", out, modPath)
}
