package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// explainModule is the Django shape across three packages: an orm package
// declares the base model's static once, entities embed it, and a page uses
// entities.Placement.Objects without importing orm itself. The statics of
// orm are reached only through the embedding, so explain must see them the
// way the build does — through the one full transpile run.
func explainModule(t *testing.T) string {
	t.Helper()
	repo, _ := filepath.Abs("../..")
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/shop\n\ngo 1.22\n\nrequire github.com/vuka-lang/vuka v0.0.0\n\nreplace github.com/vuka-lang/vuka => "+repo+"\n")
	writeFile(t, filepath.Join(root, "orm", "orm.vuka"), `package orm

import "context"

type Manager[T any] struct{ rows []*T }

func (m *Manager[T]) All(ctx context.Context) []*T { return m.rows }

type Model[Self any] struct {
	static Objects = Manager[Self]{}
}
`)
	writeFile(t, filepath.Join(root, "entities", "placement.vuka"), `package entities

import "example.com/shop/orm"

type Placement struct {
	orm.Model
	Name string
}
`)
	writeFile(t, filepath.Join(root, "pages", "page.vuka"), `package pages

import (
	"context"

	"example.com/shop/entities"
)

func List(ctx context.Context) []*entities.Placement {
	return entities.Placement.Objects.All(ctx)
}
`)
	return root
}

func TestExplainCrossPackageStatics(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOWORK", "off")
	root := explainModule(t)
	page := filepath.Join(root, "pages", "page.vuka")

	var out bytes.Buffer
	if err := explain([]string{page}, &out); err != nil {
		t.Fatalf("explain: %v", err)
	}
	if !strings.Contains(out.String(), "Model_Objects") {
		t.Errorf("explanation doesn't show the statics lowering:\n%s", out.String())
	}

	out.Reset()
	if err := explain([]string{"-full", page}, &out); err != nil {
		t.Fatalf("explain -full: %v", err)
	}
	if !strings.Contains(out.String(), "Model_Objects") {
		t.Errorf("-full output doesn't show the statics lowering:\n%s", out.String())
	}
}

func TestExplainError(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOWORK", "off")
	root := explainModule(t)
	page := filepath.Join(root, "pages", "page.vuka")
	writeFile(t, page, strings.Replace(mustRead(t, page), ".Objects.", ".Objectss.", 1))

	var out bytes.Buffer
	err := explain([]string{page}, &out)
	if err == nil {
		t.Fatalf("explain of a broken file succeeded:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "has no field Objectss") {
		t.Errorf("error doesn't name the bad reference: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("a failed explain wrote an explanation:\n%s", out.String())
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
