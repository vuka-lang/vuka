package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/ui/v2/gallery"
)

// vuka ui gallery --out writes the pages, with the app's stylesheet after
// the kit's; it needs no module.
func TestUIGalleryOut(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app.css"), []byte(":root{--ui-primary:#7c3aed}"), 0o644)
	out, err := runUI(t, dir, "gallery", "--out", "site", "--css", "app.css")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "wrote site") {
		t.Errorf("output: %s", out)
	}
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(dir, "site", filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for _, name := range []string{"index.html", "states.html", "light.html", "dark.html", "components/button.html"} {
		s := read(name)
		for _, want := range []string{"--ui-primary:#7c3aed", `data-set-theme="dark"`, `components/data-table.html"`} {
			if !strings.Contains(s, want) {
				t.Errorf("%s: no %s", name, want)
			}
		}
	}
	for _, want := range []string{`data-case="button/disabled"`, `data-case="widget/Text/error"`} {
		if !strings.Contains(read("states.html"), want) {
			t.Errorf("states.html: no %s", want)
		}
	}
	// The kit's Button page names the registry's button, which vuka ui add
	// writes; Tile has no copy to write.
	if !strings.Contains(read("components/button.html"), "vuka ui add button") {
		t.Error("the button page does not name vuka ui add button")
	}
	if strings.Contains(read("components/tile.html"), "vuka ui add") {
		t.Error("the tile page names vuka ui add")
	}
	if !strings.Contains(read("dark.html"), `<html lang="en" data-theme="dark"`) {
		t.Error("dark.html is not dark")
	}
}

func TestUIGalleryArgs(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "styles"), 0o755)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"gallery", "button"}, "takes no arguments"},
		{[]string{"gallery", "--css", "nope.css", "--out", "x"}, "--css nope.css"},
		{[]string{"gallery", "--css", "styles", "--out", "x"}, "is a directory"},
		{[]string{"gallery", "--out", "x", "--addr", ":0"}, "give one"},
		{[]string{"gallery", "--out"}, "--out needs a value"},
		{[]string{"gallery", "-f"}, "no flag -f"},
		{[]string{"galery"}, `no command "galery"`},
	} {
		_, err := runUI(t, dir, c.args...)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("vuka ui %s: %v, want %q", strings.Join(c.args, " "), err, c.want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "x")); err == nil {
		t.Error("a failed run wrote pages")
	}
}

// The served gallery reads the app's stylesheet on every request and stops
// with its context.
func TestUIGalleryServe(t *testing.T) {
	css := filepath.Join(t.TempDir(), "app.css")
	os.WriteFile(css, []byte(":root{--ui-primary:#000001}"), 0o644)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- serveGallery(ctx, ln, gallery.Options{Cases: gallery.Kit()[:4], CSSFiles: []string{css}}, &out)
	}()
	url := galleryURL(ln.Addr())
	if !strings.HasPrefix(url, "http://localhost:") {
		t.Fatalf("url %s", url)
	}
	get := func(path string) string {
		t.Helper()
		res, err := http.Get(url + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode != 200 {
			t.Fatalf("%s: %d %s", path, res.StatusCode, b)
		}
		return string(b)
	}
	if body := get(""); !strings.Contains(body, "#000001") || !strings.Contains(body, `components/button.html"`) {
		t.Fatalf("index: %s", body)
	}
	os.WriteFile(css, []byte(":root{--ui-primary:#000002}"), 0o644)
	if body := get("dark.html"); !strings.Contains(body, "#000002") || !strings.Contains(body, `data-case="actions"`) {
		t.Fatal("dark.html does not have the edited stylesheet")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), url) {
		t.Errorf("output: %s", out.String())
	}
}

func TestGalleryURL(t *testing.T) {
	for addr, want := range map[string]string{"127.0.0.1:7357": "http://localhost:7357/", "[::]:80": "http://localhost:80/", "192.168.1.5:9000": "http://192.168.1.5:9000/"} {
		a, _ := net.ResolveTCPAddr("tcp", addr)
		if got := galleryURL(a); got != want {
			t.Errorf("%s: %s, want %s", addr, got, want)
		}
	}
}
