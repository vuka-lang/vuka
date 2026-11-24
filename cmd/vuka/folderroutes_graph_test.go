package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// routeGraphApp is an app of folder routes: a root layout and home page,
// and areas, each with a layout, a guard, an index, a [item] parameter
// folder with its page and an edit page, and sections with an index each.
// Every page reads a shared package, and with links links home through urls.
// A page's markup is more than the compiler inlines, as most are.
func routeGraphApp(areas, sections int, links bool) map[string]string {
	files := map[string]string{
		"main.go":                 "package main\n\nimport (\n\t\"github.com/vuka-lang/web/v2\"\n\n\t\"example.com/app/pages\"\n)\n\nfunc main() {\n\tif err := web.New(pages.Routes()).Run(\"\"); err != nil {\n\t\tpanic(err)\n\t}\n}\n",
		"shared/shared.go":        "package shared\n\nimport \"strings\"\n\n// Title is a page's title.\nfunc Title(s string) string { return s + \" - Orders\" }\n\n// Words are a text's words.\nfunc Words(s string) []string { return strings.Fields(s) }\n",
		"pages/_layout.vuka.html": "<script lang=\"go\">\npackage pages\n\nimport \"github.com/vuka-lang/ui/v2\"\n\n// Layout wraps every page.\ntype Layout struct {\n\tChildren ui.Node\n}\n</script>\n\n<main>{Children}</main>\n",
		"pages/index.vuka.html":   graphPage("pages", "Index", "", "home", links),
	}
	for a := range areas {
		area := fmt.Sprintf("area%d", a)
		dir := "pages/" + area + "/"
		files[dir+"_layout.vuka.html"] = "<script lang=\"go\">\npackage " + area + "\n\nimport \"github.com/vuka-lang/ui/v2\"\n\n// Layout frames the area.\ntype Layout struct {\n\tChildren ui.Node\n}\n</script>\n\n<section>{Children}</section>\n"
		files[dir+"guard.go"] = "package " + area + "\n\nimport \"context\"\n\n// Guard lets a member in.\ntype Guard struct{}\n\nfunc (g *Guard) Load(ctx context.Context) error { return nil }\n"
		files[dir+"index.vuka.html"] = graphPage(area, "Index", "", area, links)
		files[dir+"[item]/index.vuka.html"] = graphPage("item", "Index", "Item int", area+" item", links)
		files[dir+"[item]/edit.vuka.html"] = graphPage("item", "Edit", "Item int", area+" edit", links)
		for s := range sections {
			sec := fmt.Sprintf("section%d", s)
			files[dir+sec+"/index.vuka.html"] = graphPage(sec, "Index", "", area+" "+sec, links)
		}
	}
	return files
}

func graphPage(pkg, typ, field, text string, links bool) string {
	imports, home := "import \"example.com/app/shared\"\n", `"/"`
	if links {
		imports, home = "import (\n\t\"example.com/app/pages/urls\"\n\t\"example.com/app/shared\"\n)\n", "urls.Root.Path()"
	}
	return "<script lang=\"go\">\npackage " + pkg + "\n\n" + imports + "\n// " + typ + " is a page.\ntype " + typ + " struct {\n\t" + field + "\n}\n\nfunc (p *" + typ + ") Title() string { return shared.Title(" + fmt.Sprintf("%q", text) + ") }\n</script>\n\n<h1>{p.Title()}</h1>\n<p>" + text + "</p>\n<ul>\n  for i, w := range shared.Words(p.Title()) {\n    <li data-i={i}>{w}</li>\n  }\n</ul>\n<a href={" + home + "}>home</a>\n"
}

// compiledPackages are the packages a go build compiled, read from its
// -debug-actiongraph file: those it didn't take from the build cache.
func compiledPackages(t *testing.T, graph string) []string {
	t.Helper()
	b, err := os.ReadFile(graph)
	if err != nil {
		t.Fatal(err)
	}
	var actions []struct {
		Mode, Package string
		Cmd           []string
	}
	if err := json.Unmarshal(b, &actions); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, a := range actions {
		if a.Mode == "build" && len(a.Cmd) > 0 && strings.HasPrefix(a.Package, "example.com/app") {
			out = append(out, a.Package)
		}
	}
	slices.Sort(out)
	return out
}

// TestRouteBuildGraph: the route files follow the tree, not the pages'
// bodies. An edit to a page's markup leaves pages/route.go and
// pages/urls/urls_gen.go as they were, compiles no package but the page's
// and route.go's, and records the route facts of the edited package alone;
// a new page compiles its own package and route.go's.
func TestRouteBuildGraph(t *testing.T) {
	if testing.Short() {
		t.Skip("builds an app three times")
	}
	app := localModule(t, webCheckout(t), routeGraphApp(2, 2, false))
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv(traceEnv, "1")
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(app, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	build := func() ([]string, string) {
		t.Helper()
		graph := filepath.Join(t.TempDir(), "graph.json")
		var stderr strings.Builder
		if err := vukaIn(app, &stderr, &stderr, "build", "-debug-actiongraph="+graph, "-o", filepath.Join(t.TempDir(), "app"), "."); err != nil {
			t.Fatalf("vuka build: %v\n%s", err, stderr.String())
		}
		return compiledPackages(t, graph), stderr.String()
	}
	build()
	build() // after the go command completed go.sum, which the transpile's cache is keyed by
	route, urls := read("pages/route.go"), read("pages/urls/urls_gen.go")

	page := filepath.Join(app, "pages/area1/section0/index.vuka.html")
	src := read("pages/area1/section0/index.vuka.html")
	writeFile(t, page, strings.Replace(src, "<p>area1 section0</p>", "<p>area1 section0, edited</p>", 1))
	got, log := build()
	if read("pages/route.go") != route || read("pages/urls/urls_gen.go") != urls {
		t.Error("an edit to a page's markup rewrote route.go or urls_gen.go")
	}
	// The package pages is compiled again, as the go command compiles a
	// package whose import's output changed; its route.go is the tree
	// alone, the same file. Nothing else that imports no edited package is.
	if want := []string{"example.com/app/pages", "example.com/app/pages/area1/section0"}; !slices.Equal(got, want) {
		t.Errorf("an edit to a page's markup compiled %v, want %v", got, want)
	}
	if facts := strings.Count(log, "route facts of "); facts != 1 || !strings.Contains(log, "route facts of example.com/app/pages/area1/section0 ") {
		t.Errorf("an edit to a page's markup recorded %d packages' route facts:\n%s", facts, log)
	}

	writeFile(t, filepath.Join(app, "pages/area0/section9/index.vuka.html"), graphPage("section9", "Index", "", "new", false))
	got, _ = build()
	if read("pages/route.go") == route {
		t.Error("a new page left route.go as it was")
	}
	// main imports route.go's package; no page imports urls here.
	want := []string{"example.com/app", "example.com/app/pages", "example.com/app/pages/area0/section9"}
	if !slices.Equal(got, want) && !slices.Equal(got, slices.Delete(slices.Clone(want), 0, 1)) {
		t.Errorf("a new page compiled %v, want %v", got, want)
	}
}
