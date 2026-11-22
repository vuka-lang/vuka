package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// petsPage is the pets view page, keeping the pet in the query; with bad,
// a declaration of its Go that doesn't type-check.
func petsPage(bad string) string {
	return "package view\n\nimport \"github.com/vuka-lang/ui/v2\"\n\n" + bad + "// Index shows a pet.\ntype Index struct {\n\tui.Live\n\tPet int64 @ui.URL(\"pet\")\n}\n\n<p>{Pet}</p>\n"
}

// ordersApp is an app whose orders module, Vuka and Go files, has a
// component and types and imports the package urls; most pages import it,
// one through a Go package.
func ordersApp(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("runs the go command")
	}
	files := map[string]string{
		"main.go":                         e2eMain,
		"models/order.go":                 "package models\n\ntype Order struct {\n\tID    int64\n\tTotal int\n}\n",
		"pages/pets/view/index.vuka.html": petsPage(""),
		"modules/orders/orders.vuka": `package orders

import (
	"context"

	"example.com/app/models"
	"example.com/app/pages/urls"
)

type Row struct {
	ID    int64
	Total int
}

func Rows() []Row { return []Row{{ID: 1, Total: models.Order{Total: 3}.Total}} }

func PetLink(ctx context.Context) string { return urls.Pets.View.URL(ctx, urls.PetsView{Pet: 1}) }
`,
		"modules/orders/total.go":        "package orders\n\nfunc Total(ns ...int) int {\n\tt := 0\n\tfor _, n := range ns {\n\t\tt += n\n\t}\n\treturn t\n}\n",
		"modules/orders/tasks.vuka.html": "package orders\n\n// Tasks lists what is left to do.\ntype Tasks struct {\n\tTitle string\n}\n\n<section><h2>{Title}</h2></section>\n",
		"reports/reports.go":             "package reports\n\nimport \"example.com/app/modules/orders\"\n\nfunc Sum() int { return orders.Total(1, 2) }\n",
		"pages/index.vuka.html":          "package pages\n\n// Index is the welcome page.\ntype Index struct{}\n\n<p>welcome</p>\n",
		"pages/home/index.vuka.html": `package home

import "example.com/app/modules/orders"

// Index is the home page.
type Index struct{}

<main><orders.Tasks title="Today"/></main>
`,
		"pages/orders/index.vuka.html": `package orders

import (
	"example.com/app/modules/orders"
	"github.com/vuka-lang/ui/v2"
)

// Index lists the orders.
type Index struct {
	ui.Live
}

<ui.DataTable rows={orders.Rows()} index>
	<slot:column header="Total" let={r}>{r.Total}</slot:column>
</ui.DataTable>
`,
		"pages/posts/index.vuka.html": `package posts

import (
	"example.com/app/modules/orders"
	"example.com/app/reports"
)

// Index lists the posts.
type Index struct{}

<a href={orders.PetLink(ctx)}>pet {reports.Sum()}</a>
`,
	}
	return localModule(t, webCheckout(t), files)
}

// TestTypeErrorStaysInItsPage: a page whose Go doesn't type-check is the one
// error vet, build and run -watch report; the packages importing the orders
// module, unrelated to the page, have none of their own. A run that doesn't
// keep the session (a Vuka package's Go doesn't compile) lists the module's
// Go packages before the orders module has transpiled.
func TestTypeErrorStaysInItsPage(t *testing.T) {
	root := ordersApp(t)
	t.Chdir(root)
	page := filepath.Join(root, "pages/pets/view/index.vuka.html")
	goCmd := func(cmd string) (string, error) {
		var stderr bytes.Buffer
		args := []string{"./..."}
		if cmd == "build" {
			args = []string{"-o", filepath.Join(t.TempDir(), "app"), "."}
		}
		err := runGo(cmd, args, io.Discard, &stderr)
		return stderr.String(), err
	}
	sess, err := load.NewSession(root)
	if err != nil {
		t.Fatal(err)
	}
	watch := func() string {
		var stderr bytes.Buffer
		s := &supervisor{root: root, dir: root, tmp: t.TempDir(), pkg: []string{"."}, flags: []string{"-buildvcs=false"}, env: os.Environ(), stderr: &stderr, sess: sess, w: &watcher{}}
		r := s.build(context.Background(), 1, &app{})
		if r.err == nil {
			return ""
		}
		s.finish(r, time.Time{})
		return stderr.String()
	}
	clean := func(when string) {
		t.Helper()
		if out, err := goCmd("vet"); err != nil {
			t.Fatalf("vet %s: %v\n%s", when, err, out)
		}
		if out := watch(); out != "" {
			t.Fatalf("run -watch %s:\n%s", when, out)
		}
	}
	clean("before the error")
	writeFile(t, page, petsPage("var count int = \"x\"\n\n"))
	check := func(name, out string) {
		t.Helper()
		switch {
		case !strings.Contains(out, "cannot use \"x\" (untyped string constant) as int value") ||
			!strings.Contains(out, "pages/pets/view/index.vuka.html:5:"):
			t.Errorf("%s doesn't report the page's error:\n%s", name, out)
		case strings.Contains(out, "undefined: orders") || strings.Contains(out, "E0105") || strings.Contains(out, "E0181") ||
			strings.Contains(out, "pages/orders/") || strings.Contains(out, "pages/home/") || strings.Contains(out, "pages/posts/"):
			t.Errorf("%s reports errors of packages that have none:\n%s", name, out)
		}
	}
	for _, cmd := range []string{"vet", "build"} {
		out, err := goCmd(cmd)
		if err == nil {
			t.Fatalf("%s passed", cmd)
		}
		check("vuka "+cmd, out)
		if !strings.HasSuffix(out, "1 error in 1 package\n") {
			t.Errorf("vuka %s's summary:\n%s", cmd, out)
		}
	}
	check("run -watch", watch())
	writeFile(t, page, petsPage(""))
	clean("once the error is fixed")
}
