package load

import (
	"path/filepath"
	"testing"
)

// goImportsVukaModule writes a module whose package entities has only .vuka
// files, imported by a plain Go package (dtos) and, after it, by a Vuka
// package (accounts) that also imports entities directly.
func goImportsVukaModule(t *testing.T) string {
	t.Helper()
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOWORK", "off")
	repo, _ := filepath.Abs("../..")
	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), "module shop\n\ngo 1.23\n\nrequire github.com/vuka-lang/vuka/v2 v2.0.0\n\nreplace github.com/vuka-lang/vuka/v2 => "+repo+"\n")
	write(t, filepath.Join(root, "entities", "models.vuka"), "package entities\n\ntype Category struct {\n\tID   int64\n\tName string\n}\n\nfunc Find(id int64) Result[Category] { return Ok(Category{ID: id}) }\n")
	write(t, filepath.Join(root, "dtos", "advert.go"), "package dtos\n\nimport \"shop/entities\"\n\ntype CategoryCount struct {\n\tCategory entities.Category\n\tCount    int\n}\n")
	write(t, filepath.Join(root, "accounts", "account.vuka"), "package accounts\n\nimport (\n\t\"shop/entities\"\n\t\"shop/dtos\"\n)\n\nfunc Count(id int64) Result[dtos.CategoryCount] {\n\tc := entities.Find(id)?\n\treturn Ok(dtos.CategoryCount{Category: c, Count: 1})\n}\n")
	return root
}

func TestGoImportsVuka(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	root := goImportsVukaModule(t)
	pkgs, err := Discover(root, "shop", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Transpile(pkgs, t.TempDir(), Options{}); err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	sess, err := NewSession(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := sess.Transpile(pkgs, t.TempDir(), Options{}); err != nil {
		t.Fatalf("Session.Transpile: %v", err)
	}
}
