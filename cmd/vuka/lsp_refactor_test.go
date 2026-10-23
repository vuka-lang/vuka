package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// refactorModule is a module of files requiring the local vuka, ui and web,
// for refactors applied and built.
func refactorModule(t *testing.T, files map[string]string) string {
	t.Helper()
	gomod, gosum := testModule(t, "example.com/app")
	repo, _ := filepath.Abs("../..")
	web := os.Getenv("VUKA_WEB")
	if web == "" {
		web = filepath.Join(filepath.Dir(repo), "vuka-web")
	}
	if _, err := os.Stat(filepath.Join(web, "go.mod")); err != nil {
		t.Skipf("the refactor tests need github.com/vuka-lang/web/v2 checked out at %s (or $VUKA_WEB)", web)
	}
	webSum, _ := os.ReadFile(filepath.Join(web, "go.sum"))
	uiMod, _ := os.ReadFile(filepath.Join(os.Getenv("VUKA_UI"), "go.mod"))
	if os.Getenv("VUKA_UI") == "" {
		uiMod, _ = os.ReadFile(filepath.Join(filepath.Dir(repo), "vuka-ui", "go.mod"))
	}
	webMod, _ := os.ReadFile(filepath.Join(web, "go.mod"))
	// Every module the build loads, at the highest version any of them
	// requires: the go.mod go accepts as it is.
	reqs := map[string]string{"github.com/vuka-lang/web/v2": "v2.0.0"}
	var order []string
	for _, text := range []string{gomod, string(uiMod), string(webMod)} {
		for _, line := range strings.Split(text, "\n") {
			f := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "require "))
			if len(f) < 2 || !strings.Contains(f[0], ".") || !strings.HasPrefix(f[1], "v") {
				continue
			}
			if v, ok := reqs[f[0]]; !ok || semverLess(v, f[1]) {
				if !ok {
					order = append(order, f[0])
				}
				reqs[f[0]] = f[1]
			}
		}
	}
	var req strings.Builder
	req.WriteString("require (\n\tgithub.com/vuka-lang/web/v2 v2.0.0\n")
	for _, m := range order {
		req.WriteString("\t" + m + " " + reqs[m] + "\n")
	}
	req.WriteString(")\n")
	start, end := strings.Index(gomod, "require ("), strings.Index(gomod, "\n)\n")+3
	gomod = gomod[:start] + req.String() + gomod[end:] + "\nreplace github.com/vuka-lang/web/v2 => " + web + "\n"
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	writeFile(t, filepath.Join(dir, "go.mod"), gomod)
	writeFile(t, filepath.Join(dir, "go.sum"), gosum+string(webSum))
	for name, src := range files {
		writeFile(t, filepath.Join(dir, name), src)
	}
	return dir
}

// apply writes a refactor's change to disk.
func apply(t *testing.T, c change) {
	t.Helper()
	for path, text := range c.result() {
		if text == nil {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			continue
		}
		writeFile(t, path, string(text))
	}
}

// vets runs vuka vet over the module in dir: the refactored code
// transpiles, compiles and vets.
func vets(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("GOWORK", "off")
	t.Chdir(dir)
	var out, errOut bytes.Buffer
	if err := runGo("vet", []string{"./..."}, &out, &errOut); err != nil {
		var files strings.Builder
		filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
			if err == nil && !fi.IsDir() && (strings.HasSuffix(p, ".vuka.html") || strings.HasSuffix(p, ".vuka") || strings.HasSuffix(p, ".go")) {
				b, _ := os.ReadFile(p)
				files.WriteString("== " + strings.TrimPrefix(p, dir+"/") + "\n" + string(b) + "\n")
			}
			return nil
		})
		t.Fatalf("vuka vet: %v\n%s%s\n%s", err, out.Bytes(), errOut.Bytes(), files.String())
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// selection is the offsets of the text from the line holding from to the
// end of the line holding to.
func selection(src, from, to string) (int, int) {
	s := strings.Index(src, from)
	s = strings.LastIndexByte(src[:s], '\n') + 1
	e := strings.Index(src[s:], to) + s + len(to)
	if nl := strings.IndexByte(src[e:], '\n'); nl >= 0 {
		e += nl
	}
	return s, e
}

const refactorMain = `package main

import (
	"example.com/app/pages"
	"github.com/vuka-lang/web/v2"
)

func main() { _ = web.New(pages.Routes()) }
`

const refactorItem = `package pages

type Item struct {
	ID   int
	Name string
}
`

const listPage = `<script lang="go">
package pages

import (
	"strings"

	"github.com/vuka-lang/ui/v2"
)

type Index struct {
	ui.Live
	items []Item
	count int
	title string
}

func (p *Index) Add() { p.count++ }

func (p *Index) Remove(id int) {}
</script>

<main>
  <h1>{strings.ToUpper(title)}</h1>
  <ul>
    for _, it := range items {
      <li key={it.ID}>
        <span>{it.Name}</span>
        <button onClick={func() { p.Remove(it.ID) }}>Remove</button>
      </li>
    }
  </ul>
  <p>{count} items</p>
  <button onClick={Add}>Add one</button>
</main>
`

// TestExtractComponent: a selection of a page's markup becomes a shape in
// _parts, in the script form, and the page passes it what it reads.
func TestExtractComponent(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a module")
	}
	dir := refactorModule(t, map[string]string{"main.go": refactorMain, "pages/item.go": refactorItem, "pages/index.vuka.html": listPage})
	path := filepath.Join(dir, "pages/index.vuka.html")
	s, e := selection(listPage, "<p>{count}", "Add one</button>")
	c, err := extractComponent(path, []byte(listPage), s, e, "Counter", os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	apply(t, c)
	part := readFile(t, filepath.Join(dir, "pages/_parts/counter.vuka.html"))
	page := readFile(t, path)
	t.Logf("part:\n%s\npage:\n%s", part, page)
	if !strings.HasPrefix(part, "<script lang=\"go\">\npackage parts\n") {
		t.Errorf("not the script form")
	}
	vets(t, dir)
}

// TestExtractRow: a loop's item extracted from a page in the Go-header
// form: the item, a method called through the receiver and the key; the
// row needs the page's type, so it stays in the page's package. The key
// stays on the element the row renders, as the compiler asks of a struct
// component.
func TestExtractRow(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a module")
	}
	header := strings.Replace(strings.Replace(listPage, "<script lang=\"go\">\n", "", 1), "</script>\n\n", "", 1)
	dir := refactorModule(t, map[string]string{"main.go": refactorMain, "pages/item.go": refactorItem, "pages/index.vuka.html": header})
	path := filepath.Join(dir, "pages/index.vuka.html")
	s, e := selection(header, "<li key", "</li>")
	c, err := extractComponent(path, []byte(header), s, e, "row", os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	apply(t, c)
	part := readFile(t, filepath.Join(dir, "pages/_row.vuka.html"))
	page := readFile(t, path)
	for _, want := range []string{"type Row struct {\n\tItem   Item\n\tRemove func(id int)\n}", "<li key={Item.ID}>", "onClick={func() { Remove(Item.ID) }}"} {
		if !strings.Contains(part, want) {
			t.Errorf("the row has no %q:\n%s", want, part)
		}
	}
	if !strings.Contains(page, "<Row Item={it} Remove={Remove} />") || strings.Contains(page, "private_parts") {
		t.Errorf("page:\n%s", page)
	}
	vets(t, dir)
}
