package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scaffolded writes an app vuka new would make (without its dependencies) into a
// temporary directory.
func scaffolded(t *testing.T, flags map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	s := &scaffold{Dir: dir, Module: "example.com/shop", Name: "Shop"}
	if err := s.answer(flags, nil); err != nil {
		t.Fatal(err)
	}
	s.AdminEmail, s.AdminPassword = "admin@example.com", "pw"
	files, err := s.render()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeFiles(dir, files); err != nil {
		t.Fatal(err)
	}
	return dir
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAddModule(t *testing.T) {
	dir := scaffolded(t, map[string]string{})
	files, err := addModule(dir, "example.com/shop", "invoices")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"migrations/0002_invoices.go", "modules/invoices/invoices_test.vuka", "modules/invoices/model.vuka",
		"modules/invoices/page.vuka.html", "modules/invoices/pages.vuka", "modules/invoices/service.vuka", "main.vuka (imports the module)"}
	if strings.Join(files, ",") != strings.Join(want, ",") {
		t.Errorf("wrote %v", files)
	}
	for path, s := range map[string]string{
		"modules/invoices/model.vuka":         "type Invoice struct",
		"modules/invoices/service.vuka":       "type Invoices struct{}",
		"modules/invoices/pages.vuka":         `@web.Live("/invoices", web.Layout(layouts.Page("Invoices")))`,
		"migrations/0002_invoices.go":         `Dependencies: []string{"0001_initial"}`,
		"modules/invoices/invoices_test.vuka": "func TestInvoices(t *testing.T)",
	} {
		if !strings.Contains(read(t, filepath.Join(dir, path)), s) {
			t.Errorf("%s lacks %q", path, s)
		}
	}
	main := read(t, filepath.Join(dir, "main.vuka"))
	if !strings.Contains(main, "\t_ \"example.com/shop/modules/invoices\"\n\t\"example.com/shop/modules/users\"") {
		t.Errorf("main.vuka doesn't import the module among the app's packages:\n%s", main)
	}

	// The next one depends on it; the same one again is refused, as are
	// names that can't be packages.
	if _, err := addModule(dir, "example.com/shop", "tickets"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, filepath.Join(dir, "migrations/0003_tickets.go")), `"0002_invoices"`) {
		t.Error("0003 doesn't depend on 0002")
	}
	for name, msg := range map[string]string{
		"invoices": "exists already",
		"users":    "has it already",
		"Notes":    "lower-case",
		"my-notes": "lower-case",
		"func":     "lower-case",
	} {
		if _, err := addModule(dir, "example.com/shop", name); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("%s: %v, want %q", name, err, msg)
		}
	}
	if strings.Count(read(t, filepath.Join(dir, "main.vuka")), "modules/invoices") != 1 {
		t.Error("a refused add changed main.vuka")
	}
}

// Each kind of app gets the module its pages are made of.
func TestAddModuleKinds(t *testing.T) {
	templ := scaffolded(t, map[string]string{"ui": "templ", "auth": "no"})
	if _, err := addModule(templ, "example.com/shop", "notes"); err != nil {
		t.Fatal(err)
	}
	page := read(t, filepath.Join(templ, "modules/notes/pages.vuka"))
	if !strings.Contains(page, `@web.Template("notes.templ")`) || strings.Contains(page, "auth") {
		t.Errorf("templ, no sign-in:\n%s", page)
	}
	if !strings.Contains(read(t, filepath.Join(templ, "modules/notes/notes.templ")), "templ PageView(rows []Note") {
		t.Error("the templ view")
	}
	if !strings.Contains(read(t, filepath.Join(templ, "migrations/0001_notes.go")), `Name: "0001_notes"`) {
		t.Error("the first migration of an app without one")
	}

	api := scaffolded(t, map[string]string{"ui": "api"})
	if _, err := addModule(api, "example.com/shop", "categories"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, filepath.Join(api, "modules/categories/api.vuka")), `@web.Get("/api/categories")`) {
		t.Error("the API's routes")
	}
	if !strings.Contains(read(t, filepath.Join(api, "modules/categories/model.vuka")), "type Category struct") {
		t.Error("categories' model")
	}

	nodb := scaffolded(t, map[string]string{"db": "none"})
	if _, err := addModule(nodb, "example.com/shop", "notes"); err == nil || !strings.Contains(err.Error(), "no database") {
		t.Errorf("an app without a database: %v", err)
	}
}

func TestSingular(t *testing.T) {
	for in, want := range map[string]string{"notes": "Note", "categories": "Category", "boxes": "Box", "addresses": "Address", "news": "New", "sheep": "Sheep", "glass": "Glass", "matches": "Match"} {
		if got := singular(in); got != want {
			t.Errorf("singular(%q) = %q, want %q", in, got, want)
		}
	}
}
