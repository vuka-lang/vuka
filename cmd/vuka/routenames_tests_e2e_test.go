package main

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const rnLinks = `package links

import (
	"context"

	"github.com/vuka-lang/web/v2"
)

// ItemURL links to an item's page.
func ItemURL(ctx context.Context, id int) string {
	return web.RouteURL(ctx, "items", id) + "?tab=info"
}
`

const rnLinksTest = `package links

import (
	"context"
	"testing"

	"github.com/vuka-lang/web/v2"
)

func TestItemURL(t *testing.T) {
	ctx := context.Background()
	if got, want := ItemURL(ctx, 7), web.Route(ctx, "items", 7)+"?tab=info"; got != want || got != "/items/7?tab=info" {
		t.Fatalf("ItemURL = %q, want %q", got, want)
	}
}
`

const rnLinksXTest = `package links_test

import (
	"context"
	"testing"

	"github.com/vuka-lang/web/v2"
)

func TestQuery(t *testing.T) {
	if got := web.Route(context.Background(), "items", 3, web.RouteQuery{"task": 9}); got != "/items/3?task=9" {
		t.Fatal(got)
	}
}
`

const rnOrders = `package orders

// Total is a sum.
func Total(xs ...int) (n int) {
	for _, x := range xs {
		n += x
	}
	return n
}
`

// rnOrdersTest is an internal test of a package that imports no web.
const rnOrdersTest = `package orders

import "testing"

func TestTotal(t *testing.T) {
	if Total(1, 2) != 3 {
		t.Fatal(Total(1, 2))
	}
}
`

// TestVukaTestRouteNames: under vuka test a component package's tests,
// internal and external, link by route name though they build no app;
// packages without tests stay without, and vuka build's binary has none of
// it.
func TestVukaTestRouteNames(t *testing.T) {
	if !hasWebFunc(webDirForTest(), "RegisterRouteNames") {
		needs(t, "github.com/vuka-lang/web/v2 has no RegisterRouteNames; set VUKA_WEB")
	}
	root := testBuildsModule(t, map[string]string{
		"components/links/links.go":      rnLinks,
		"components/links/links_test.go": rnLinksTest,
		"components/links/x_test.go":     rnLinksXTest,
		"orders/orders.go":               rnOrders,
		"orders/orders_test.go":          rnOrdersTest,
	})
	var out, errOut bytes.Buffer
	if err := runGo("test", []string{"-count=1", "./..."}, &out, &errOut); err != nil {
		t.Fatalf("vuka test ./...: %v\n%s%s", err, out.Bytes(), errOut.Bytes())
	}
	for _, want := range []string{"ok  \texample.com/app/components/links", "ok  \texample.com/app/orders", "ok  \texample.com/app/pages/items/param_id", "example.com/app/layouts\t[no test files]"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("vuka test ./... has no %q:\n%s", want, out.Bytes())
		}
	}
	errOut.Reset()
	if err := runGo("vet", []string{"./..."}, io.Discard, &errOut); err != nil {
		t.Fatalf("vuka vet ./...: %v\n%s", err, errOut.Bytes())
	}
	bin := filepath.Join(t.TempDir(), "app")
	errOut.Reset()
	if err := runGo("build", []string{"-o", bin, "."}, io.Discard, &errOut); err != nil {
		t.Fatalf("vuka build: %v\n%s", err, errOut.Bytes())
	}
	syms, err := exec.Command("go", "tool", "nm", bin).Output()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(syms, []byte("RegisterRouteNames")) {
		t.Error("vuka build's binary links RegisterRouteNames")
	}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Name() == routeNamesTestGo {
			t.Errorf("%s is on disk", p)
		}
		return nil
	})
}

func webDirForTest() string {
	if web := os.Getenv("VUKA_WEB"); web != "" {
		return web
	}
	repo, _ := filepath.Abs("../..")
	return filepath.Join(filepath.Dir(repo), "vuka-web")
}
