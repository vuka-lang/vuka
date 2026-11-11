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
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// A component with a directory testdata/uidocs/<name>/ gets a showcase
// page: a hero preview, installation, usage and one section per variant,
// each a live preview of an example beside its source, and the API
// reference. page.md is the page's text between the installation and the
// API reference, and these lines in it stand for an example:
//
//	<!-- example size -->   a preview of size.vuka.html and its source
//	<!-- code usage -->     usage.vuka.html's source alone
//	<!-- related badge card -->  the components listed under Related
//
// Its first paragraph is the page's description. Each example is a
// .vuka.html file built with the registry's components (ejectRegistry), so
// a snippet is the source of what its preview shows. demo.vuka.html is the
// hero. The previews are static HTML, rendered by the components and drawn
// with the Tailwind CSS built from them and the design tokens, in
// docs/public/ui/previews.

const (
	uiPreviews = "../../docs/public/ui/previews"
	uiPages    = "../../docs/public/ui/pages"
)

var (
	exampleLine = regexp.MustCompile(`(?m)^<!-- (example|code|related) ([^>]*?) -->\n`)
	structName  = regexp.MustCompile(`(?m)^type (\w+) struct`)
)

// showcaseExamples renders every example of the module at dir
// (components/examples/<component>/<name>.vuka.html, built), by
// "<component>/<name>", with a program it writes there.
func showcaseExamples(t *testing.T, dir string) map[string]string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "components", "examples", "*", "*.vuka.html"))
	if len(files) == 0 {
		return nil
	}
	var imports, calls strings.Builder
	pkgs := map[string]bool{}
	for _, f := range files {
		comp, name := filepath.Base(filepath.Dir(f)), strings.TrimSuffix(filepath.Base(f), ".vuka.html")
		alias := "ex_" + strings.ReplaceAll(comp, "-", "_")
		if !pkgs[comp] {
			pkgs[comp] = true
			fmt.Fprintf(&imports, "\t%s %q\n", alias, "example.com/app/components/examples/"+comp)
		}
		src, _ := os.ReadFile(f)
		m := structName.FindSubmatch(src)
		if m == nil {
			t.Fatalf("%s: no struct", f)
		}
		fmt.Fprintf(&calls, "\trender(%q, &%s.%s{})\n", comp+"/"+name, alias, m[1])
	}
	main := "package main\n\nimport (\n\t\"context\"\n\t\"encoding/json\"\n\t\"os\"\n\n\t\"github.com/vuka-lang/ui/v2\"\n\n" + imports.String() + ")\n\n" +
		"var out = map[string]string{}\n\nfunc render(name string, n ui.Node) {\n\ts, err := ui.String(context.Background(), n)\n\tif err != nil {\n\t\tpanic(name + \": \" + err.Error())\n\t}\n\tout[name] = s\n}\n\n" +
		"func main() {\n" + calls.String() + "\tjson.NewEncoder(os.Stdout).Encode(out)\n}\n"
	writeFile(t, filepath.Join(dir, "cmd", "render", "main.go"), main)
	cmd := exec.Command("go", "run", "./cmd/render")
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	b, err := cmd.Output()
	if err != nil {
		t.Fatalf("rendering the examples: %v\n%s", err, stderr.String())
	}
	out := map[string]string{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// previewDoc is an example's preview page: its HTML drawn with the
// previews' stylesheet, following the docs' theme.
func previewDoc(html string) string {
	return "<!doctype html>\n<html lang=\"en\" data-theme=\"light\">\n<head>\n<meta charset=\"utf-8\">\n<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">\n" +
		"<link rel=\"stylesheet\" href=\"../preview.css\">\n<script src=\"../preview.js\"></script>\n</head>\n<body>\n" + strings.TrimSpace(html) + "\n</body>\n</html>\n"
}

// previewJS keeps a preview in the docs' theme (VitePress's class "dark"
// on its <html>), as the components' tokens read it: data-theme.
const previewJS = `(function () {
  var root = document.documentElement
  function sync() {
    var dark = false
    try { dark = parent.document.documentElement.classList.contains('dark') } catch (e) {}
    root.dataset.theme = dark ? 'dark' : 'light'
  }
  sync()
  try { new MutationObserver(sync).observe(parent.document.documentElement, { attributes: true, attributeFilter: ['class'] }) } catch (e) {}
})()
`

// previewCSSHead marks the stylesheet with the Tailwind that built it:
// another version builds other bytes, so only the same one checks them.
const previewCSSHead = "/* Written by go test -run TestUIDocs -update in cmd/vuka with %s. */\n"

// previewCSS is the Tailwind CSS of the previews (the HTML files in
// dir) with the registry's design tokens, and the Tailwind version that
// built it; "" when there is no Tailwind CLI.
func previewCSS(t *testing.T, dir string) (css, version string) {
	t.Helper()
	tw, err := exec.LookPath("tailwindcss")
	if err != nil {
		return "", ""
	}
	v, _ := exec.Command(tw, "--help").CombinedOutput()
	version = strings.TrimSpace(strings.TrimPrefix(strings.SplitN(string(v), "\n", 2)[0], "≈ "))
	tokens, _ := registryFS.ReadFile("registry/styles/default.css")
	in := "@import \"tailwindcss\" source(none);\n@source \"./pages\";\n\n" + string(tokens) +
		"\nhtml, body { height: 100%; margin: 0; }\n" +
		"body { display: flex; align-items: center; justify-content: center; box-sizing: border-box; padding: 2.5rem 1.5rem;\n" +
		"  background: var(--background); color: var(--foreground);\n" +
		"  font: 14px/1.5 ui-sans-serif, system-ui, -apple-system, \"Segoe UI\", sans-serif; -webkit-font-smoothing: antialiased; }\n" +
		"*, ::before, ::after { border-color: var(--border); }\n"
	writeFile(t, filepath.Join(dir, "in.css"), in)
	cmd := exec.Command(tw, "-i", "in.css", "-o", "out.css", "--minify")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tailwindcss: %v\n%s", err, out)
	}
	b, err := os.ReadFile(filepath.Join(dir, "out.css"))
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf(previewCSSHead, version) + string(b) + "\n", version
}

// showcase is what a showcase page is made of.
type showcase struct {
	page, copy string            // the page, and as plain Markdown
	previews   map[string]string // preview pages, by path in uiPreviews
}

// showcasePage is a component's showcase page; prev and next are the
// components before and after it.
func showcasePage(t *testing.T, here string, reg *registry, it *registryItem, text string, rendered map[string]string, ejected map[string][]byte, prev, next *registryItem) showcase {
	t.Helper()
	sc := showcase{previews: map[string]string{}}
	exDir := filepath.Join(here, "testdata", "uidocs", it.Name)
	source := func(name string) string {
		b, err := os.ReadFile(filepath.Join(exDir, name+".vuka.html"))
		if err != nil {
			t.Errorf("%s: no example %s: write testdata/uidocs/%s/%s.vuka.html", it.Name, name, it.Name, name)
		}
		return strings.TrimRight(string(b), "\n")
	}
	used := map[string]bool{}
	// example is the example's block, and code its source alone; each
	// also as plain Markdown.
	example := func(name string, hero bool) (string, string) {
		used[name] = true
		html, ok := rendered[it.Name+"/"+name]
		if !ok {
			t.Errorf("%s: example %s did not render", it.Name, name)
		}
		sc.previews[it.Name+"/"+name+".html"] = previewDoc(html)
		attr := ""
		if hero {
			attr = " hero"
		}
		src := markup(source(name))
		return fmt.Sprintf("<UiExample src=\"/ui/previews/%s/%s.html\"%s>\n\n```vuka-html:line-numbers\n%s\n```\n\n</UiExample>\n", it.Name, name, attr, src),
			"```vuka-html\n" + src + "\n```\n"
	}
	code := func(name string) string {
		used[name] = true
		return "```vuka-html\n" + source(name) + "\n```\n"
	}

	desc, body, _ := strings.Cut(strings.TrimSpace(text), "\n\n")
	desc = strings.Join(strings.Fields(desc), " ")
	var related []string
	var page, plain strings.Builder
	rest := body + "\n"
	for {
		loc := exampleLine.FindStringSubmatchIndex(rest)
		if loc == nil {
			page.WriteString(rest)
			plain.WriteString(rest)
			break
		}
		page.WriteString(rest[:loc[0]])
		plain.WriteString(rest[:loc[0]])
		kind, arg := rest[loc[2]:loc[3]], rest[loc[4]:loc[5]]
		switch kind {
		case "example":
			b, p := example(arg, false)
			page.WriteString(b)
			plain.WriteString(p)
		case "code":
			c := code(arg)
			page.WriteString(c)
			plain.WriteString(c)
		case "related":
			related = strings.Fields(arg)
		}
		rest = rest[loc[1]:]
	}
	if files, _ := filepath.Glob(filepath.Join(exDir, "*.vuka.html")); len(files) > 0 {
		for _, f := range files {
			if name := strings.TrimSuffix(filepath.Base(f), ".vuka.html"); name != "demo" && !used[name] {
				t.Errorf("testdata/uidocs/%s/%s is on no page: name it in page.md", it.Name, filepath.Base(f))
			}
		}
	}

	var b, c strings.Builder
	fmt.Fprintf(&b, "---\ntitle: %s\ndescription: %q\npageClass: vk-ui-component vk-ui-showcase\noutline: [2, 3]\n---\n\n<!-- Written by go test -run TestUIDocs -update in cmd/vuka, from its registry and testdata/uidocs/%s. -->\n\n", it.Title, desc, it.Name)
	fmt.Fprintf(&b, "# %s\n\n", it.Title)
	actions := fmt.Sprintf("<UiPageActions copy=\"/ui/pages/%s.md\"", it.Name)
	if prev != nil {
		actions += fmt.Sprintf(" prev=\"/ui/components/%s\" prev-title=%q", prev.Name, prev.Title)
	}
	if next != nil {
		actions += fmt.Sprintf(" next=\"/ui/components/%s\" next-title=%q", next.Name, next.Title)
	}
	b.WriteString(actions + " />\n\n")
	b.WriteString(mdText(desc) + "\n\n")
	fmt.Fprintf(&c, "# %s\n\n%s\n\n", it.Title, desc)
	hero, heroPlain := example("demo", true)
	b.WriteString(hero + "\n")
	c.WriteString(heroPlain + "\n")

	// Installation: the command, or the files to copy.
	var files []string
	var manual strings.Builder
	for _, f := range it.Files {
		_, name, _ := strings.Cut(f.Path, "/")
		files = append(files, "`components/ui/"+name+"`")
		src, _ := registryFS.ReadFile("registry/" + f.Path)
		label := "Manual"
		if len(it.Files) > 1 {
			label += ": " + name
		}
		fmt.Fprintf(&manual, "```%s:line-numbers [%s]\n%s```\n", langOf(name), label, src)
	}
	var deps []string
	for _, d := range it.Deps {
		deps = append(deps, fmt.Sprintf("[%s](/ui/vendored#%s)", reg.item(d).Title, d))
	}
	install := fmt.Sprintf("## Installation\n\n::: code-group\n\n```sh [Command]\nvuka ui add %s\n```\n\n%s\n:::\n\n", it.Name, manual.String())
	install += fmt.Sprintf("The command writes %s into the app as its own source", strings.Join(files, " and "))
	if len(deps) > 0 {
		install += ", with what it uses: " + strings.Join(deps, ", ")
	}
	install += ". To copy it by hand, save the file there"
	if len(deps) > 0 {
		install += " and add what it uses"
	}
	install += "; [UI Components (Vendored)](/ui/vendored) covers the design tokens they draw with.\n\n"
	b.WriteString(install)
	fmt.Fprintf(&c, "## Installation\n\n```sh\nvuka ui add %s\n```\n\n", it.Name)

	b.WriteString(strings.TrimRight(page.String(), "\n") + "\n\n")
	c.WriteString(strings.TrimRight(plain.String(), "\n") + "\n\n")

	api := showcaseAPI(t, it, ejected, desc)
	b.WriteString(api)
	c.WriteString(api)
	if len(related) > 0 {
		var links []string
		for _, r := range related {
			if reg.item(r) == nil {
				t.Errorf("%s: related %s is no component", it.Name, r)
				continue
			}
			links = append(links, fmt.Sprintf("[%s](/ui/components/%s)", reg.item(r).Title, r))
		}
		b.WriteString("## Related\n\n" + strings.Join(links, " · ") + "\n")
	}
	sc.page, sc.copy = b.String(), c.String()
	return sc
}

// markup is an example's markup: the file after its script block, the
// part a snippet teaches (Usage shows the imports).
func markup(src string) string {
	if _, after, ok := strings.Cut(src, "</script>\n"); ok {
		return strings.TrimSpace(after)
	}
	return src
}

func langOf(name string) string {
	if strings.HasSuffix(name, ".vuka.html") {
		return "vuka-html"
	}
	return "go"
}

// showcaseAPI is the API reference: a table per exported struct of the
// component's files, its prop, type and default.
func showcaseAPI(t *testing.T, it *registryItem, ejected map[string][]byte, desc string) string {
	fset := token.NewFileSet()
	var files []*ast.File
	for _, f := range it.Files {
		_, name, _ := strings.Cut(f.Path, "/")
		src, _ := registryFS.ReadFile("registry/" + f.Path)
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		if strings.HasSuffix(name, ".vuka.html") {
			src = ejected[name]
		}
		file, err := parser.ParseFile(fset, name, src, parser.ParseComments)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		files = append(files, file)
	}
	zero := map[string]string{} // a type → its constant whose value is ""
	var structs []*ast.TypeSpec
	docs := map[*ast.TypeSpec]string{}
	for _, f := range files {
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok {
				continue
			}
			var typ string
			for _, s := range g.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					if _, ok := s.Type.(*ast.StructType); ok && s.Name.IsExported() {
						structs = append(structs, s)
						docs[s] = g.Doc.Text()
						if s.Doc != nil {
							docs[s] = s.Doc.Text()
						}
					}
				case *ast.ValueSpec:
					if id, ok := s.Type.(*ast.Ident); ok {
						typ = id.Name
					}
					for i, n := range s.Names {
						if g.Tok == token.CONST && typ != "" && i < len(s.Values) {
							if lit, ok := s.Values[i].(*ast.BasicLit); ok && lit.Value == `""` {
								zero[typ] = n.Name
							}
						}
					}
				}
			}
		}
	}
	slices.SortStableFunc(structs, func(a, b *ast.TypeSpec) int {
		switch {
		case a.Name.Name == it.Title:
			return -1
		case b.Name.Name == it.Title:
			return 1
		}
		return 0
	})
	var b strings.Builder
	b.WriteString("## API Reference\n\n")
	for _, s := range structs {
		lead := leadSentence(firstParagraph(docs[s]))
		if s.Name.Name == it.Title {
			lead = leadSentence(desc)
		}
		fmt.Fprintf(&b, "### %s\n\n%s\n\n| Prop | Type | Default |\n|---|---|---|\n", s.Name.Name, mdText(lead))
		for _, f := range s.Type.(*ast.StructType).Fields.List {
			if len(f.Names) == 0 || !f.Names[0].IsExported() {
				continue
			}
			var tb bytes.Buffer
			printer.Fprint(&tb, fset, f.Type)
			typ := tb.String()
			for _, n := range f.Names {
				attr := propName(n.Name)
				if n.Name == "Children" {
					attr = "children"
				}
				fmt.Fprintf(&b, "| `%s` | `%s` | %s |\n", attr, mdText(typ), fieldDefault(typ, f.Doc.Text()+" "+f.Comment.Text(), zero))
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

var theDefault = regexp.MustCompile(`("[^"]*") \((?:the )?default\)`)

// fieldDefault is a prop's default: its type's constant with the value
// "", the value its doc calls the default, false, or a dash.
func fieldDefault(typ, doc string, zero map[string]string) string {
	if c := zero[typ]; c != "" {
		return "`" + c + "`"
	}
	if m := theDefault.FindStringSubmatch(strings.Join(strings.Fields(doc), " ")); m != nil {
		return "`" + m[1] + "`"
	}
	if typ == "bool" {
		return "`false`"
	}
	return "-"
}

func leadSentence(s string) string {
	s = firstSentence(s)
	if s != "" && !strings.HasSuffix(s, ".") {
		s = strings.TrimSuffix(s, ":") + "."
	}
	return s
}

// checkUIPreviews compares the showcase pages' files under docs/public/ui
// (pub: previews/…, pages/…) with want, and the previews' stylesheet and
// script, writing them with -update. The stylesheet is checked only by
// the Tailwind version that wrote it.
func checkUIPreviews(t *testing.T, pub, modDir string, want map[string]string) {
	t.Helper()
	if len(want) == 0 {
		return
	}
	want["previews/preview.js"] = previewJS
	// Tailwind reads the previews from the module's pages directory.
	for p, doc := range want {
		if strings.HasPrefix(p, "previews/") && strings.HasSuffix(p, ".html") {
			writeFile(t, filepath.Join(modDir, "pages", filepath.FromSlash(p)), doc)
		}
	}
	css, version := previewCSS(t, modDir)
	cssPath := filepath.Join(pub, "previews", "preview.css")
	switch old, _ := os.ReadFile(cssPath); {
	case css == "":
		if *updateFiles {
			needs(t, "no Tailwind CLI: the previews' stylesheet needs it")
		}
		t.Log("no Tailwind CLI: docs/public/ui/previews/preview.css not checked")
	case !bytes.HasPrefix(old, []byte(fmt.Sprintf(previewCSSHead, version))) && !*updateFiles:
		t.Logf("docs/public/ui/previews/preview.css was written by another Tailwind than %s: not checked", version)
	default:
		want["previews/preview.css"] = css
	}
	for _, dir := range []string{"previews", "pages"} {
		filepath.WalkDir(filepath.Join(pub, dir), func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(pub, p)
			rel = filepath.ToSlash(rel)
			if _, ok := want[rel]; !ok && rel != "previews/preview.css" {
				if *updateFiles {
					os.Remove(p)
				} else {
					t.Errorf("docs/public/ui/%s is no showcase page's: go test -run TestUIDocs -update", rel)
				}
			}
			return nil
		})
	}
	for _, rel := range slices.Sorted(mapKeys(want)) {
		p := filepath.Join(pub, filepath.FromSlash(rel))
		if old, err := os.ReadFile(p); err == nil && string(old) == want[rel] {
			continue
		}
		if *updateFiles {
			writeFile(t, p, want[rel])
			continue
		}
		t.Errorf("docs/public/ui/%s is out of date: go test -run TestUIDocs -update", rel)
	}
}
