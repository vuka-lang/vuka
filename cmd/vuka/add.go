package main

import (
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

	"github.com/vuka-lang/vuka/internal/load"
)

const addUsage = `Usage:

	vuka add module <name>
	vuka add service <module>/<Name>
	vuka add component <Name> [--module m]
	vuka add island <Name> [--vue|--react|--svelte] [--dir dir] [--no-install]
	vuka add page <path> [--module m] [--live] [--island Name [--vue|--react|--svelte]] [--name Type]
	vuka add entity <module>/<Name> [field:type…] [--table name]

Grow an app vuka new made.

  module     modules/<name>/: a model, a service, a page (or API routes), a
             test, a migration making its table, and main.vuka's import of
             it. The name is a plural noun in lower case: notes, invoices.
  service    a @web.Service in a module, with a static New and a test: a
             dependency pages and routes take as a parameter or field.
  component  a stateless component taking a props struct, in components/
             (or the module's package).
  island     a client component in the island frontend (web/islands/ by
             default): Vue (the default, or the frontend's framework),
             React or Svelte, its props typed so an import reads them. The
             framework's packages and Vite plugin are added when missing,
             and installed (--no-install: not).
  page       a page in pages/ (or the module's package): @web.Get, or
             @web.Live with --live. --island Name makes it a page backed by
             a component: an @ui.Island struct with a prop, a url: field
             and a method, and the component calling it (live with --live).
  entity     an orm model in a module, with ID and created/updated times,
             its fields name:type — string, text, int, float, decimal,
             bool, time, date, json, ref (a foreign key to the model the
             name names: user:ref) — ? for an optional one (published_at:time?),
             :unique or :index after (email:string:unique); a test, and its
             migration from go tool orm makemigrations.

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
	return append(written, "main.vuka (imports the module)"), nil
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

// registerImport adds a blank import of pkg to main.vuka's import block,
// so the module's routes, pages and services are declared.
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
