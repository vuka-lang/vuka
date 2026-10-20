package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/transpile"
)

const assetsPage = `package main

import (
	"example.com/app/views"

	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

@web.Live("/counter",
	web.Layout(page, web.Style("layout.css")),
	web.Style("counter.css", "parts/*.css"),
	web.Style("counter.css", "print.css", web.Global),
	web.Script("hooks/**/*.ts"),
	web.CSS(` + "`" + `p { margin: 0 }` + "`" + `),
)
@doc("A counter.")
type Counter struct {
	ui.Live
	n int
}

func (c *Counter) Render() ui.Node { return <div><output>{c.n}</output></div> }

func page(p ui.Node) ui.Node { return <main>{p}</main> }

@web.Live("/shell", web.Layout(views.Shell, web.Style("views/shell.css")))
type Plain struct{ ui.Live }

func (p *Plain) Render() ui.Node { return <p /> }

func main() { web.New(web.Options(web.Style("site.css"))).Run("") }
`

// TestFindAssets reads the asset options of a module: several files per
// call, repeated calls (a file listed twice taken once), globs in name
// order, inline sources, web.Global, layouts of .vuka files and templ
// components, options through web.Options.
func TestFindAssets(t *testing.T) {
	root := tempModule(t, map[string]string{"go.mod": webMod, "main.vuka": assetsPage,
		"counter.css": "", "parts/b.css": "", "parts/a.css": "", "print.css": "", "layout.css": "", "site.css": "",
		"hooks/a.ts": "", "hooks/deep/b.ts": "", "hooks/c.js": "", "views/shell.css": "",
		"views/shell.templ": "package views\n\ntempl Shell(page templ.Component) {\n\t<body>@page</body>\n}\n"})
	pa, err := findAssets(root, "example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*assetComp{}
	for _, c := range pa.comps {
		byName[c.name] = c
	}
	got := func(srcs []assetSrc) []string {
		var out []string
		for _, s := range srcs {
			if s.path == "" {
				out = append(out, "inline "+s.inline+" at "+s.label)
				continue
			}
			out = append(out, s.label)
		}
		return out
	}
	for _, c := range []struct {
		name            string
		styles, scripts []string
	}{
		{"Counter", []string{"counter.css", "parts/a.css", "parts/b.css", "inline p { margin: 0 } at main.vuka:15"}, []string{"hooks/a.ts", "hooks/deep/b.ts"}},
		{"page", []string{"layout.css"}, nil},
		{"Shell", []string{"views/shell.css"}, nil},
	} {
		comp := byName[c.name]
		if comp == nil {
			t.Fatalf("no component %s: %v", c.name, pa.comps)
		}
		if !slices.Equal(got(comp.styles), c.styles) || !slices.Equal(got(comp.scripts), c.scripts) {
			t.Errorf("%s: %q %q, want %q %q", c.name, got(comp.styles), got(comp.scripts), c.styles, c.scripts)
		}
	}
	if s := byName["Shell"]; s.dir != filepath.Join(root, "views") || s.templ == "" || s.id != scopeID("example.com/app/views", "Shell") {
		t.Errorf("Shell: %+v", s)
	}
	if want := []string{"counter.css", "print.css", "site.css"}; !slices.Equal(got(pa.global.styles), want) {
		t.Errorf("global: %q, want %q", got(pa.global.styles), want)
	}
	sc := pa.scopes()[root]
	if sc["Counter"] != scopeID("example.com/app", "Counter") || sc["page"] == "" || len(sc) != 2 {
		t.Errorf("scopes: %v", pa.scopes())
	}
}

func TestFindAssetsErrors(t *testing.T) {
	for _, c := range []struct{ name, decl, want string }{
		{"missing", `web.Style("countr.css")`, "error[E0605]: web.Style(\"countr.css\"): no such file\n --> main.vuka:7\n  = help: did you mean counter.css?"},
		{"glob", `web.Script("hook/*.ts")`, "error[E0605]: web.Script(\"hook/*.ts\"): matches no file\n --> main.vuka:7"},
		{"glob near", `web.Style("*.scss")`, "error[E0605]: web.Style(\"*.scss\"): matches no file\n --> main.vuka:7"},
		{"kind", `web.Script("counter.css")`, "error[E0605]: web.Script(\"counter.css\"): not a .ts, .js, .mts, .mjs file\n --> main.vuka:7"},
		{"literal", `web.Style(name)`, "error[E0604]: web.Style takes string literals (the vuka command reads them from the source)\n --> main.vuka:7"},
		{"layout literal", `web.Layout(func(p ui.Node) ui.Node { return p }, web.Style("counter.css"))`, `the layout must be a named function`},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := "package main\n\nimport \"github.com/vuka-lang/web\"\n\nconst name = \"x\"\n\n@web.Live(\"/\", " + c.decl + ")\ntype C struct{}\n"
			root := tempModule(t, map[string]string{"go.mod": webMod, "main.vuka": src, "counter.css": "", "hooks/a.ts": ""})
			_, err := findAssets(root, "example.com/app")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("got %v, want %s", err, c.want)
			}
		})
	}
	root := tempModule(t, map[string]string{"go.mod": webMod, "counter.css": "",
		"main.vuka": "package main\n\nimport \"github.com/vuka-lang/web\"\n\nvar o = web.Style(\"counter.css\")\n"})
	if _, err := findAssets(root, "example.com/app"); err == nil || !strings.Contains(err.Error(), "error[E0604]: web.Style belongs to no page: give it to @web.Live, web.Layout, web.LiveLayout or web.New\n --> main.vuka:5") {
		t.Errorf("an option of no page: %v", err)
	}
}

// TestScopeCSS: selectors match the component's own elements; :global and
// :root stay unscoped, :deep reaches into children, :scope is the root;
// nested rules, groups and keyframes.
func TestScopeCSS(t *testing.T) {
	const s = `[data-vk-s="abc"]`
	for _, c := range []struct{ in, want string }{
		{"output { color: red }", "output" + s + " { color: red }"},
		{".a .b, .c > .d { x: y }", ".a .b" + s + ", .c > .d" + s + " { x: y }"},
		{".a:hover::before { x: y }", ".a:hover" + s + "::before { x: y }"},
		{"li:after { x: y }", "li" + s + ":after { x: y }"},
		{".a:not(.b .c) { x: y }", ".a:not(.b .c)" + s + " { x: y }"},
		{"[type=\"a b\"] { x: y }", "[type=\"a b\"]" + s + " { x: y }"},
		{":global(.dark) .a { x: y }", ".dark .a" + s + " { x: y }"},
		{":global(body.x) { x: y }", "body.x { x: y }"},
		{".list :deep(li) { x: y }", ".list" + s + " li { x: y }"},
		{":deep(.x) { x: y }", s + " .x { x: y }"},
		{":root { --g: 1 }", ":root { --g: 1 }"},
		{":root.dark .a { x: y }", ":root.dark .a" + s + " { x: y }"},
		{":scope { x: y }", s + ":not(" + s + " *) { x: y }"},
		{":scope > p { x: y }", s + ":not(" + s + " *) > p" + s + " { x: y }"},
		{".a { color: red; &:hover { x: y } .b { x: y } > .c { x: y } }",
			".a" + s + " { color: red; &:hover { x: y } .b" + s + " { x: y } > .c" + s + " { x: y } }"},
		{"@media (min-width: 40rem) { .a { x: y } }", "@media (min-width: 40rem) { .a" + s + " { x: y } }"},
		{"@layer components { .a { @apply p-4; } }", "@layer components { .a" + s + " { @apply p-4; } }"},
		{".a { @media print { display: none } }", ".a" + s + " { @media print { display: none } }"},
		{"@font-face { font-family: X; src: url(x.woff) }", "@font-face { font-family: X; src: url(x.woff) }"},
		{"@keyframes bump { from { opacity: 0 } to { opacity: 1 } }\n.a { animation: bump 1s; }",
			"@keyframes bump-abc { from { opacity: 0 } to { opacity: 1 } }\n.a" + s + " { animation: bump-abc 1s; }"},
		{".a { animation-name: other; }", ".a" + s + " { animation-name: other; }"},
		{"/* c */ .a { x: y }", "/* c */ .a" + s + " { x: y }"},
	} {
		frames := map[string]string{}
		keyframesOf(c.in, "abc", frames)
		got, _, err := scopeCSS(c.in, "/src", "x.css", 0, "abc", frames)
		if err != nil || got != c.want {
			t.Errorf("%s:\n got %s (%v)\nwant %s", c.in, got, err, c.want)
		}
	}
	for _, c := range []struct{ in, want string }{
		{".a { x: y }\n.b { x: y", "x.css:2: unclosed {"},
		{".a { x: y }\n}", "x.css:2: unexpected }"},
		{"\n\n.a { content: \"x }", "x.css:3: unclosed string"},
		{".a { x: y }\n/* open", "x.css:2: unclosed comment"},
		{"\n{ x: y }", "x.css:2: a rule needs a selector before {"},
		{".a;\n.b { x: y }", "x.css:1: expected { after .a"},
	} {
		if _, _, err := scopeCSS(c.in, "/src", "x.css", 0, "abc", nil); err == nil || err.Error() != c.want {
			t.Errorf("%q: %v, want %s", c.in, err, c.want)
		}
	}
	if _, _, err := scopeCSS("\n.a {", "/src", "main.vuka", 11, "abc", nil); err == nil || err.Error() != "main.vuka:13: unclosed {" {
		t.Errorf("inline CSS: %v", err)
	}
	if got, _, _ := scopeCSS(`@import "./parts/x.css";`, "/src", "x.css", 0, "abc", nil); got != `@import "/src/parts/x.css";` {
		t.Errorf("@import: %s", got)
	}
}

// TestSelectorWarnings: a selector requiring a class, id or element the
// component's markup (or its scripts) never writes is reported, with the
// nearest name it does write.
func TestSelectorWarnings(t *testing.T) {
	src := `package main

import (
	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

@web.Live("/", web.Style("c.css"), web.Script("c.ts"))
type C struct{ ui.Live }

func (c *C) Render() ui.Node {
	return <div class={box}><output id="count" class="tabular-nums">0</output><Child /></div>
}

const box = "box flex"

func (c *C) helper() ui.Node { return <section /> }
`
	css := ".box output { x: y }\n.bmp { x: y }\n#cont { x: y }\nspan { x: y }\n:global(.zz) { x: y }\n.list :deep(.qq) { x: y }\n.flash, section { x: y }\n"
	root := tempModule(t, map[string]string{"go.mod": webMod, "main.vuka": src, "c.css": css,
		"c.ts": "export default { mounted(el: HTMLElement) { el.classList.add(\"bump\", \"flash\") } }\n"})
	pa, err := findAssets(root, "example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	var warn bytes.Buffer
	if _, err := compileAssets(pa, false, &warn); err != nil {
		t.Fatal(err)
	}
	want := "c.css:2: .bmp matches no element in C; did you mean .bump?\n" +
		"c.css:3: #cont matches no element in C; did you mean #count?\n" +
		"c.css:4: span matches no element in C\n" +
		"c.css:6: .list matches no element in C\n"
	if warn.String() != want {
		t.Errorf("warnings:\n%s\nwant:\n%s", warn.String(), want)
	}

	// A template's scoped style reads the template's markup.
	root = tempModule(t, map[string]string{"go.mod": webMod,
		"card.go":        "package main\n\ntype Card struct{ Title string }\n",
		"card.vuka.html": "package main\n\n<section class=\"card\"><h3>{Title}</h3></section>\n\n<style scoped>\nh3 { x: y }\n.card { x: y }\nspan { x: y }\n</style>\n"})
	if pa, err = findAssets(root, "example.com/app"); err != nil {
		t.Fatal(err)
	}
	warn.Reset()
	if _, err := compileAssets(pa, false, &warn); err != nil {
		t.Fatal(err)
	}
	if want := "card.vuka.html:8: span matches no element in Card\n"; warn.String() != want {
		t.Errorf("a template's warnings:\n%s\nwant:\n%s", warn.String(), want)
	}
}

// TestBundleScripts bundles TypeScript with its relative imports into one
// module listing each script's hooks in order, and lists the methods.
func TestBundleScripts(t *testing.T) {
	root := tempModule(t, map[string]string{"go.mod": webMod,
		"a.ts":        "import { twice } from \"./lib/math\";\nexport default {\n\tmounted(el: HTMLElement): void { el.dataset.n = String(twice(2)) },\n\tbump(el: HTMLElement, by: number = 1) { return by },\n};\n",
		"lib/math.ts": "export const twice = (n: number): number => n * 2;\n",
		"b.js":        "const hooks = { updated() {}, async load() {}, shake: () => 1 };\nexport default hooks;\n",
		"bad.ts":      "import confetti from \"canvas-confetti\";\nexport default { go() { confetti() } };\n",
		"dup.ts":      "export default { bump() {} };\n",
		"broken.ts":   "export default {\n\tmounted(el {\n};\n",
	})
	comp := func(files ...string) *assetComp {
		c := &assetComp{importPath: "example.com/app", name: "C", id: "abc"}
		for _, f := range files {
			c.scripts = append(c.scripts, assetSrc{path: filepath.Join(root, f), label: f, dir: root})
		}
		return c
	}
	b, err := bundleScripts(root, comp("a.ts", "b.js"), false)
	if err != nil {
		t.Fatal(err)
	}
	js := string(b.js)
	if !regexp.MustCompile(`^abc\.[0-9a-f]{8}\.js$`).MatchString(b.name) || !strings.Contains(js, "export") ||
		strings.Contains(js, "HTMLElement") || strings.Contains(js, "import") || !strings.Contains(js, "*2") {
		t.Errorf("bundle %s:\n%s", b.name, js)
	}
	if !slices.Equal(b.methods, []string{"bump", "load", "shake"}) {
		t.Errorf("methods: %v", b.methods)
	}
	inline := comp()
	inline.scripts = []assetSrc{{inline: "export default { hi() { return 1 as number } }", label: "main.vuka:9", dir: root}}
	if b, err := bundleScripts(root, inline, true); err != nil || !slices.Equal(b.methods, []string{"hi"}) || !strings.Contains(string(b.js), "sourceMappingURL") {
		t.Errorf("inline: %v %v", b, err)
	}
	for _, c := range []struct {
		files []string
		want  string
	}{
		{[]string{"bad.ts"}, `bad.ts:1:22: Could not resolve "canvas-confetti"`},
		{[]string{"a.ts", "dup.ts"}, `app.C: a.ts and dup.ts both export the method bump`},
		{[]string{"broken.ts"}, `broken.ts:2: Expected ")" but found "{"`},
	} {
		if _, err := bundleScripts(root, comp(c.files...), false); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want %s", c.files, err, c.want)
		}
	}
}

// assetsModule is a module on the local web, ui and vuka, skipping the test
// when that web doesn't take compiled assets.
func assetsModule(t *testing.T, files map[string]string) string {
	t.Helper()
	repo, _ := filepath.Abs("../..")
	web := os.Getenv("VUKA_WEB")
	if web == "" {
		web = filepath.Join(filepath.Dir(repo), "vuka-web")
	}
	if !hasWebFunc(web, "RegisterAssets") {
		needs(t, "github.com/vuka-lang/web at %s has no RegisterAssets; set VUKA_WEB (and VUKA_UI)", web)
	}
	return localModule(t, web, files)
}

// localModule writes files as a module requiring web (at web), and the
// local ui ($VUKA_UI, else ../vuka-ui) and vuka (this repository).
func localModule(t *testing.T, web string, files map[string]string) string {
	t.Helper()
	repo, _ := filepath.Abs("../..")
	ui := os.Getenv("VUKA_UI")
	if ui == "" {
		ui = filepath.Join(filepath.Dir(repo), "vuka-ui")
	}
	webSum, _ := os.ReadFile(filepath.Join(web, "go.sum"))
	uiSum, _ := os.ReadFile(filepath.Join(ui, "go.sum"))
	ownSum, _ := os.ReadFile("go.sum")
	files["go.mod"] = "module example.com/app\n\ngo 1.25.0\n\nrequire (\n\tgithub.com/vuka-lang/web v0.0.0\n\tgithub.com/vuka-lang/ui v0.0.0\n\tgithub.com/vuka-lang/vuka " +
		transpile.RuntimeVersion + "\n)\n\nreplace github.com/vuka-lang/web => " + web + "\n\nreplace github.com/vuka-lang/ui => " + ui +
		"\n\nreplace github.com/vuka-lang/vuka => " + repo + "\n"
	files["go.sum"] = string(webSum) + string(uiSum) + string(ownSum)
	return tempModule(t, files)
}

const e2ePage = `package main

import (
	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

@web.Live("/", web.Style("counter.css"), web.Script("counter.ts"))
type Counter struct {
	ui.Live
	n int
}

func (c *Counter) Render() ui.Node {
	return <div><output>{c.n}</output><Label /></div>
}

@web.Live("/label", web.CSS(` + "`" + `output { color: rgb(1, 2, 3) }` + "`" + `))
type Label struct{ ui.Live }

func (l *Label) Render() ui.Node { return <output>label</output> }

func main() { web.New().Run("") }
`

// TestBuildAssets: vuka build compiles a component's scoped stylesheet
// into app.css and its script into a module the binary serves under
// /_vuka/c/; its elements carry the scope and a child component's don't.
// vuka gen -inplace writes the files a plain go build needs.
func TestBuildAssets(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	if _, err := exec.LookPath("tailwindcss"); err != nil {
		needs(t, "no tailwindcss on PATH")
	}
	root := assetsModule(t, map[string]string{"main.vuka": e2ePage,
		"counter.css": "output { @apply font-bold; }\n",
		"counter.ts":  "export default { bump(el: HTMLElement) { el.classList.add(\"bump\") } };\n"})
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv(twEnv, "")
	t.Chdir(root)
	app := filepath.Join(t.TempDir(), "app")
	var stderr bytes.Buffer
	if err := runGo("build", []string{"-o", app, "."}, io.Discard, &stderr); err != nil {
		t.Fatalf("vuka build: %v\n%s", err, stderr.Bytes())
	}
	for _, f := range []string{twGoName, assetsGoName} {
		if _, err := os.Stat(filepath.Join(root, f)); err == nil {
			t.Errorf("vuka build wrote %s into the source tree", f)
		}
	}
	serve := func() func(string) (string, http.Header) {
		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		port := ln.Addr().(*net.TCPAddr).Port
		ln.Close()
		cmd := exec.Command(app)
		cmd.Env = append(os.Environ(), fmt.Sprint("PORT=", port), twCSSEnv+"=", assetsEnv+"=")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
		return func(path string) (string, http.Header) {
			var res *http.Response
			waitFor(t, func() bool {
				var err error
				res, err = http.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
				return err == nil
			})
			defer res.Body.Close()
			b, _ := io.ReadAll(res.Body)
			return string(b), res.Header
		}
	}
	check := func(get func(string) (string, http.Header)) {
		t.Helper()
		page, _ := get("/")
		counter, label := scopeID("example.com/app", "Counter"), scopeID("example.com/app", "Label")
		if !strings.Contains(page, `<output data-vk-s="`+counter+`">0</output>`) || !strings.Contains(page, `<output data-vk-s="`+label+`" data-vk-id="c2">label</output>`) ||
			!strings.Contains(page, `<div data-vk-s="`+counter+`"`) {
			t.Errorf("the page's elements aren't marked with their own component's scope: %s", page)
		}
		css, _ := get(regexp.MustCompile(`/_vuka/app\.css\?v=[^"]+`).FindString(page))
		for _, want := range []string{`output\[data-vk-s="?` + counter + `"?\]\{[^}]*font-weight:var\(--font-weight-bold\)`, `output\[data-vk-s="?` + label + `"?\]\{color:#010203\}`} {
			if !regexp.MustCompile(want).MatchString(css) {
				t.Errorf("app.css lacks %s", want)
			}
		}
		loader := regexp.MustCompile(`/_vuka/c/loader\.js\?v=[^"]+`).FindString(page)
		js, _ := get(loader)
		mod := regexp.MustCompile(`/_vuka/c/` + counter + `\.[0-9a-f]+\.js`).FindString(js)
		if loader == "" || mod == "" || !strings.Contains(js, `"bump"`) {
			t.Fatalf("the page's loader doesn't name the module: %s\n%s", loader, js)
		}
		body, hdr := get(mod)
		if !strings.Contains(body, `classList.add("bump")`) || !strings.Contains(hdr.Get("Cache-Control"), "immutable") {
			t.Errorf("module %s: %v\n%s", mod, hdr, body)
		}
	}
	check(serve())

	// vuka gen -inplace writes the files, for plain go build.
	if err := gen([]string{"-inplace"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if gen, err := os.ReadFile(filepath.Join(root, assetsGoName)); err != nil || !bytes.Contains(gen, []byte("RegisterScript(")) {
		t.Errorf("vuka gen -inplace: %s: %v", assetsGoName, err)
	}
	if out, err := exec.Command("go", "build", "-o", app, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build after vuka gen -inplace: %v\n%s", err, out)
	}
	check(serve())
}

// TestTemplScopes: a scoped templ component's elements carry its scope —
// in its branches and loops, and the children it passes another component
// — as constant attributes; another component's don't.
func TestTemplScopes(t *testing.T) {
	src := `package views

templ Shell(items []string, page templ.Component) {
	<html><body>
		if len(items) > 0 {
			<ul>
				for _, it := range items {
					<li>{ it }</li>
				}
			</ul>
		}
		@Card() {
			<p>passed</p>
		}
		@page
	</body></html>
}

templ Card() {
	<section>{ children... }</section>
}
`
	out, _, err := compileTempl("v.templ", "v.templ", []byte(src), map[string]string{"Shell": "s1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, el := range []string{"html", "body", "ul", "li", "p"} {
		if !strings.Contains(string(out), `<`+el+` data-vk-s=\"s1\">`) {
			t.Errorf("<%s> isn't marked:\n%s", el, out)
		}
	}
	if !strings.Contains(string(out), `<section>`) {
		t.Errorf("Card's <section> is marked:\n%s", out)
	}
}

// TestScriptPushes: the methods a component's scripts push by name are the
// only ones the browser may call by name; a name that isn't a literal, or
// that isn't a method of the component, stops the build.
func TestScriptPushes(t *testing.T) {
	src := `package main

import (
	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

@web.Live("/", web.Layout(page, web.Script("layout.ts")), web.Script("ok.ts"))
type C struct{ ui.Live }

func (c *C) Render() ui.Node   { return <p /> }
func (c *C) Inc()              {}
func (c *C) Save(f Form) error { return nil }
func (c *C) Mount()            {}

func page(p ui.Node) ui.Node { return <main>{p}</main> }
`
	root := tempModule(t, map[string]string{"go.mod": webMod, "main.vuka": src,
		"ok.ts":      "export default { mounted(el, ctx) { ctx.push(\"Inc\"); ctx.push('Save', { a: 1 }); ctx.push(`Inc`); list.push(x); /* ctx.push(y) */ } };\n",
		"dyn.ts":     "export default {\n\tmounted(el, ctx) {\n\t\tconst name = \"Inc\";\n\t\tctx.push(name);\n\t},\n};\n",
		"unknown.ts": "export default {\n\tmounted(el, ctx) { ctx.push(\"Incr\"); ctx.push(\"Mount\") },\n};\n",
		"layout.ts":  "export default { mounted(el, ctx) { ctx.push(\"Inc\") } };\n"})
	pa, err := findAssets(root, "example.com/app")
	if err != nil {
		t.Fatal(err)
	}
	var comp, layout *assetComp
	for _, c := range pa.comps {
		if c.name == "C" {
			comp = c
		} else {
			layout = c
		}
	}
	b, err := bundleScripts(root, comp, false)
	if err != nil || !slices.Equal(b.pushes, []string{"Inc", "Save"}) {
		t.Fatalf("pushes: %v %v", b, err)
	}
	out, err := compileAssets(pa, false, io.Discard)
	if err == nil {
		t.Fatalf("a layout's script pushed: %s", out.manifest)
	}
	if !strings.Contains(err.Error(), `layout.ts:1: ctx.push("Inc"): app.page is no live component`) {
		t.Errorf("layout: %v", err)
	}
	layout.scripts = nil
	if out, err = compileAssets(pa, false, io.Discard); err != nil || !strings.Contains(string(out.manifest), `"type":"main.C","pushes":["Inc","Save"]`) {
		t.Errorf("manifest: %s %v", out.manifest, err)
	}
	for file, want := range map[string]string{
		"dyn.ts":     "dyn.ts:4: ctx.push needs the method's name as a string literal",
		"unknown.ts": "unknown.ts:2: ctx.push(\"Incr\"): C has no method Incr; did you mean Inc?\nunknown.ts:2: ctx.push(\"Mount\"): C has no method Mount",
	} {
		comp.scripts = []assetSrc{{path: filepath.Join(root, file), label: file, dir: root}}
		if _, err := bundleScripts(root, comp, false); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %s", file, err, want)
		}
	}
}
