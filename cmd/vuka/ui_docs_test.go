package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// uiDocs is where the components' pages go, beside the docs they link to.
const uiDocs = "../../docs/ui/components"

// The components' pages, docs/ui/components/<name>.md, an index and the
// sidebar's entries, made from the registry, testdata/uidocs/<name>.md (each
// one's usage) and the Go vuka eject turns each component into. A change
// to any of them without the pages written again fails here: go test -run
// TestUIDocs -update writes them. The screenshots come from
// TestUIComponentsE2E.
func TestUIDocs(t *testing.T) {
	reg, err := readRegistry(regSource{})
	if err != nil {
		t.Fatal(err)
	}
	here, _ := os.Getwd()
	docs, public := filepath.Join(here, uiDocs), filepath.Join(here, "../../docs/public/ui/components")
	_, ejected := ejectRegistry(t, reg)
	want := map[string]string{}
	var side []map[string]string
	var index strings.Builder
	index.WriteString(uiDocsHead("Components", "The components vuka ui add writes into an app.") +
		"# Components\n\n" +
		"Each component below is a file `vuka ui add` writes into your app, its own\n" +
		"source from then on. [UI Components (Vendored)](/ui/vendored) covers\n" +
		"starting, theming and updating them.\n\n" +
		"| Component | Runs | |\n|---|---|---|\n")
	for _, name := range reg.components() {
		it := reg.item(name)
		usage, err := os.ReadFile(filepath.Join(here, "testdata/uidocs", name+".md"))
		if err != nil {
			t.Errorf("%s: no usage: write testdata/uidocs/%s.md", name, name)
			continue
		}
		want[name+".md"] = uiDocsPage(t, reg, it, string(usage), ejected)
		side = append(side, map[string]string{"text": it.Title, "link": "/ui/components/" + name})
		fmt.Fprintf(&index, "| [%s](/ui/components/%s) | %s | %s |\n", it.Title, name, tierNames[it.Tier], mdText(sentence(it.Description)))
		for _, theme := range []string{"light", "dark"} {
			if !exists(public, name+"-"+theme+".png") {
				t.Errorf("no docs/public/ui/components/%s-%s.png: VUKA_SHOTS=dir go test -run TestUIComponentsE2E, then copy dir/*-light.png and dir/*-dark.png there", name, theme)
			}
		}
	}
	want["index.md"] = index.String()
	b, _ := json.MarshalIndent(side, "", "  ")
	want["sidebar.json"] = string(b) + "\n"

	have, _ := filepath.Glob(filepath.Join(docs, "*"))
	for _, p := range have {
		if want[filepath.Base(p)] == "" && !strings.HasSuffix(p, "README") {
			if *updateFiles {
				os.Remove(p)
			} else {
				t.Errorf("docs/ui/components/%s is no component's page: go test -run TestUIDocs -update", filepath.Base(p))
			}
		}
	}
	for _, name := range slices.Sorted(mapKeys(want)) {
		p := filepath.Join(docs, name)
		if old, err := os.ReadFile(p); err == nil && string(old) == want[name] {
			continue
		}
		if *updateFiles {
			writeFile(t, p, want[name])
			continue
		}
		t.Errorf("docs/ui/components/%s is out of date: go test -run TestUIDocs -update", name)
	}
}

var tierNames = []string{"", "in the browser", "on the server", "as an island"}

// ejectRegistry is the Go vuka eject writes for each of the registry's
// .vuka.html files, by the file's name: the files built against the ui
// checkout ($VUKA_UI, else vuka-ui beside this repository), written in
// the module at dir (the working directory) beside the components, in
// components/ui.
func ejectRegistry(t *testing.T, reg *registry) (dir string, _ map[string][]byte) {
	t.Helper()
	repo, _ := filepath.Abs("../..")
	ui := os.Getenv("VUKA_UI")
	if ui == "" {
		ui = filepath.Join(filepath.Dir(repo), "vuka-ui")
	}
	if _, err := os.Stat(filepath.Join(ui, "go.mod")); err != nil {
		needs(t, "no checkout of github.com/vuka-lang/ui at %s; set VUKA_UI", ui)
	}
	ui, _ = filepath.Abs(ui)
	dir, _ = filepath.EvalSymlinks(t.TempDir())
	writeFile(t, filepath.Join(dir, "go.mod"), fmt.Sprintf("module example.com/app\n\ngo 1.25\n\nrequire (\n\t%s v2.0.0\n\t%s v2.0.0\n)\n\nreplace %s => %s\n\nreplace %s => %s\n",
		uiModPath, vukaModPath, uiModPath, ui, vukaModPath, repo))
	var sums []byte
	for _, d := range []string{ui, repo} {
		s, _ := os.ReadFile(filepath.Join(d, "go.sum"))
		sums = append(sums, s...)
	}
	writeFile(t, filepath.Join(dir, "go.sum"), string(sums))
	for _, it := range reg.Items {
		for _, f := range it.Files {
			b, _ := registryFS.ReadFile("registry/" + f.Path)
			writeFile(t, filepath.Join(dir, "components", filepath.FromSlash(f.Path)), string(b))
		}
	}
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(dir)
	_, gens, err := inPlace([]string{"./..."}, nil, load.Options{})
	if err != nil {
		t.Fatalf("vuka gen: %v", err)
	}
	out := map[string][]byte{}
	for _, g := range gens {
		target := g.Target
		if !filepath.IsAbs(target) {
			target = filepath.Join(dir, target)
		}
		writeFile(t, target, string(g.Src))
		if strings.HasSuffix(g.Source, ".vuka.html") {
			out[filepath.Base(g.Source)] = ejected(g.Src)
		}
	}
	return dir, out
}

func uiDocsHead(title, desc string) string {
	return fmt.Sprintf("---\ntitle: %s\ndescription: %q\n---\n\n<!-- Written by go test -run TestUIDocs -update in cmd/vuka, from its registry and testdata/uidocs. -->\n\n", title, desc)
}

// uiDocsPage is a component's page.
func uiDocsPage(t *testing.T, reg *registry, it *registryItem, usage string, ejected map[string][]byte) string {
	var b strings.Builder
	b.WriteString(strings.Replace(uiDocsHead(it.Title, sentence(it.Description)), "---\n\n", "pageClass: vk-ui-component\naside: false\n---\n\n", 1))
	fmt.Fprintf(&b, "# %s\n\n%s\n\n", it.Title, mdText(sentence(it.Description)))
	fmt.Fprintf(&b, "<div class=\"vk-ui-shots\">\n  <img src=\"/ui/components/%[1]s-light.png\" alt=\"%[2]s in the light theme\">\n  <img src=\"/ui/components/%[1]s-dark.png\" alt=\"%[2]s in the dark theme\">\n</div>\n\n", it.Name, it.Title)

	b.WriteString("## Adding It\n\n```sh\nvuka ui add " + it.Name + "\n```\n\n")
	var files, uses []string
	for _, f := range it.Files {
		_, name, _ := strings.Cut(f.Path, "/")
		files = append(files, "`components/ui/"+name+"`")
	}
	for _, d := range it.Deps {
		if dep := reg.item(d); dep.Tier > 0 {
			uses = append(uses, fmt.Sprintf("[%s](/ui/components/%s)", dep.Title, d))
		} else {
			uses = append(uses, fmt.Sprintf("[%s](/ui/vendored#%s)", dep.Title, d))
		}
	}
	fmt.Fprintf(&b, "It writes %s", strings.Join(files, " and "))
	if len(uses) > 0 {
		fmt.Fprintf(&b, ", with what it uses: %s", strings.Join(uses, ", "))
	}
	fmt.Fprintf(&b, ". It runs %s ([the tiers](/ui/vendored#tiers))", tierNames[it.Tier])
	if len(it.Requires) > 0 {
		var reqs []string
		for _, lib := range slices.Sorted(mapKeys(it.Requires)) {
			reqs = append(reqs, fmt.Sprintf("`%s` %s", requireModules[lib], it.Requires[lib]))
		}
		fmt.Fprintf(&b, " and needs %s or newer", strings.Join(reqs, " and "))
	}
	b.WriteString(".\n\n## Using It\n\n" + strings.TrimSpace(usage) + "\n\n")

	// The props, from the Go: the component's struct, then the file's
	// other types and functions.
	fset := token.NewFileSet()
	var decls []*ast.File
	var sources []struct{ name, lang, src string }
	for _, f := range it.Files {
		_, name, _ := strings.Cut(f.Path, "/")
		src, _ := registryFS.ReadFile("registry/" + f.Path)
		switch {
		case strings.HasSuffix(name, "_test.go"):
			continue
		case strings.HasSuffix(name, ".vuka.html"):
			goSrc := ejected[name]
			if goSrc == nil {
				t.Errorf("%s: no Go ejected", name)
				continue
			}
			sources = append(sources, struct{ name, lang, src string }{name, "vuka-html", string(src)},
				struct{ name, lang, src string }{strings.TrimSuffix(name, ".vuka.html") + "_html.go", "go", string(goSrc)})
			src = goSrc
		default:
			sources = append(sources, struct{ name, lang, src string }{name, "go", string(src)})
		}
		file, err := parser.ParseFile(fset, name, src, parser.ParseComments)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		decls = append(decls, file)
	}
	b.WriteString(uiDocsProps(fset, decls, it.Title))

	b.WriteString("## Source\n\nThe component as `vuka ui add` writes it, and the Go [`vuka eject`](/tools/cli#eject) turns it into: the same component, built with `go build` alone.\n\n")
	b.WriteString("<div class=\"vk-ui-source\">\n\n::: code-group\n\n")
	var rest []struct{ name, lang, src string }
	for _, s := range sources {
		if s.lang == "go" && !strings.HasSuffix(s.name, "_html.go") {
			rest = append(rest, s)
			continue
		}
		fmt.Fprintf(&b, "```%s [%s]\n%s```\n\n", s.lang, s.name, s.src)
	}
	b.WriteString(":::\n\n</div>\n")
	for _, s := range rest {
		fmt.Fprintf(&b, "\nBeside it, the same in both: `%s`.\n\n```go\n%s```\n", s.name, s.src)
	}
	return b.String()
}

// uiDocsProps is the props tables of the component's struct (named
// title) and the other exported structs, and its exported functions.
func uiDocsProps(fset *token.FileSet, files []*ast.File, title string) string {
	consts := map[string][]string{} // a type → its constants
	var structs []*ast.TypeSpec
	var docs = map[*ast.TypeSpec]string{}
	var funcs []*ast.FuncDecl
	for _, f := range files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.GenDecl:
				var typ string
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						if _, ok := s.Type.(*ast.StructType); ok && s.Name.IsExported() {
							structs = append(structs, s)
							docs[s] = d.Doc.Text()
						}
					case *ast.ValueSpec:
						if d.Tok != token.CONST {
							continue
						}
						if id, ok := s.Type.(*ast.Ident); ok {
							typ = id.Name
						}
						for _, n := range s.Names {
							if typ != "" && n.IsExported() {
								consts[typ] = append(consts[typ], "`"+n.Name+"`")
							}
						}
					}
				}
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.IsExported() {
					funcs = append(funcs, d)
				}
			}
		}
	}
	slices.SortStableFunc(structs, func(a, b *ast.TypeSpec) int {
		switch {
		case a.Name.Name == title:
			return -1
		case b.Name.Name == title:
			return 1
		}
		return 0
	})
	expr := func(e ast.Expr) string {
		var buf bytes.Buffer
		printer.Fprint(&buf, fset, e)
		return buf.String()
	}
	var b strings.Builder
	b.WriteString("## Props\n\n")
	for i, s := range structs {
		if i > 0 {
			fmt.Fprintf(&b, "### %s\n\n%s\n\n", s.Name.Name, mdText(firstParagraph(docs[s])))
		}
		b.WriteString("| Prop | Type | |\n|---|---|---|\n")
		for _, f := range s.Type.(*ast.StructType).Fields.List {
			if len(f.Names) == 0 || !f.Names[0].IsExported() {
				continue
			}
			name, typ := f.Names[0].Name, expr(f.Type)
			doc := strings.Join(strings.Fields(f.Doc.Text()+" "+f.Comment.Text()), " ")
			if cs := consts[typ]; len(cs) > 0 {
				doc = strings.TrimSpace(doc + " One of " + strings.Join(cs, ", ") + ".")
			}
			if f.Tag != nil && strings.Contains(reflect.StructTag(strings.Trim(f.Tag.Value, "`")).Get("vuka"), "required") {
				doc = strings.TrimSpace("Required. " + doc)
			}
			attr := "`" + propName(name) + "`"
			switch {
			case name == "Children" || i > 0 && typ == "ui.Node":
				attr = "the content"
			case typ == "ui.Node" || typ == "[]"+strings.TrimSuffix(name, "s"):
				attr = "`<slot:" + propName(strings.TrimSuffix(name, "s")) + ">`"
			}
			fmt.Fprintf(&b, "| %s | `%s` | %s |\n", attr, typ, mdText(doc))
		}
		b.WriteString("\n")
	}
	if len(funcs) > 0 {
		b.WriteString("### Functions\n\n")
		for _, f := range funcs {
			sig := strings.TrimPrefix(expr(f.Type), "func")
			fmt.Fprintf(&b, "- `%s%s`: %s\n", f.Name.Name, sig, mdText(strings.Join(strings.Fields(f.Doc.Text()), " ")))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// propName is a field's name as an attribute: ID id, OnClose onClose.
func propName(s string) string {
	if strings.ToUpper(s) == s {
		return strings.ToLower(s)
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// mdText is text for Markdown that Vue renders: tags shown as text.
func mdText(s string) string {
	return strings.NewReplacer("<", "&lt;", ">", "&gt;", "|", `\|`, "{{", "{ {").Replace(s)
}

func firstParagraph(doc string) string {
	p, _, _ := strings.Cut(strings.TrimSpace(doc), "\n\n")
	return strings.Join(strings.Fields(p), " ")
}

// sentence is a description as a sentence: a capital and a full stop.
func sentence(s string) string {
	if s == "" {
		return s
	}
	s = strings.ToUpper(s[:1]) + s[1:]
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}
