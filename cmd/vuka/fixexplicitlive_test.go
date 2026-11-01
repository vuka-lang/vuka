package main

import (
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/transpile"
)

func TestEmbedLive(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"an event, the import there", "<script lang=\"go\">\npackage p\n\nimport \"github.com/vuka-lang/ui/v2\"\n\nvar _ ui.Node\n\ntype C struct {\n\tn int\n}\n\nfunc (c *C) Inc() { c.n++ }\n</script>\n\n<button onClick={Inc}>{n}</button>\n",
			"type C struct {\n\tui.Live\n\tn int\n}"},
		{"an event, no import", "<script lang=\"go\">\npackage p\n\ntype C struct{}\n\nfunc (c *C) Inc() {}\n</script>\n\n<button onClick={Inc}>x</button>\n",
			"import \"github.com/vuka-lang/ui/v2\"\n\ntype C struct{\n\tui.Live\n}"},
		{"a named import", "<script lang=\"go\">\npackage p\n\nimport u \"github.com/vuka-lang/ui/v2\"\n\nvar _ u.Node\n\ntype C struct {\n\tn int\n}\n\nfunc (c *C) Inc() {}\n</script>\n\n<button onClick={Inc}>x</button>\n",
			"\tu.Live\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, why := transpile.EmbedLive([]byte(c.src))
			if !strings.Contains(string(out), c.want) || why == "" {
				t.Fatalf("got %q:\n%s", why, out)
			}
			s, err := transpile.ParseShape(out)
			if err != nil {
				t.Fatal(err)
			}
			if !s.EmbedsLive {
				t.Errorf("not embedded:\n%s", out)
			}
			if again, _ := transpile.EmbedLive(out); again != nil {
				t.Errorf("not idempotent:\n%s", again)
			}
		})
	}
	for _, src := range []string{
		"<p>markup and no struct</p>\n",
		"package p\n\n<p>markup and no struct</p>\n",
		"<script lang=\"go\">\npackage p\n\ntype C struct{}\n</script>\n\n<p>x</p>\n",
		"<script lang=\"go\">\npackage p\n\ntype C struct{}\n\nfunc (c *C) inc() {}\n</script>\n\n<button onClick={inc}>x</button>\n",
	} {
		if out, _ := transpile.EmbedLive([]byte(src)); out != nil {
			t.Errorf("changed a shape that isn't live:\n%s", out)
		}
	}
}

// TestEmbedLiveHelpers: ui.Live goes in the struct the file renders, never a
// helper, and a helper's hook or methods alone change nothing.
func TestEmbedLiveHelpers(t *testing.T) {
	const live = "<script lang=\"go\">\npackage p\n\nimport \"github.com/vuka-lang/ui/v2\"\n\nvar _ ui.Node\n\ntype Row struct {\n\tn int\n}\n\ntype Orders struct {\n\tn int\n}\n\nfunc (o *Orders) Inc() { o.n++ }\n</script>\n\n<button onClick={Inc}>{n}</button>\n"
	out, why := transpile.EmbedLiveFile("pages/orders.vuka.html", []byte(live))
	if !strings.Contains(string(out), "type Orders struct {\n\tui.Live\n\tn int") || !strings.Contains(string(out), "type Row struct {\n\tn int") || why != "the markup sends the event Inc" {
		t.Errorf("%q:\n%s", why, out)
	}
	if again, _ := transpile.EmbedLiveFile("orders.vuka.html", out); again != nil {
		t.Errorf("not idempotent:\n%s", again)
	}
	const quiet = "<script lang=\"go\">\npackage p\n\nimport \"github.com/vuka-lang/ui/v2\"\n\ntype Row struct {\n\tn int\n}\n\n@ui.Mount\nfunc (r *Row) mount() error { return nil }\n\nfunc (r *Row) Inc() { r.n++ }\n\ntype Orders struct {\n\tRows []Row\n}\n</script>\n\n<button onClick={Rows[0].Inc}>x</button>\n"
	if out, why := transpile.EmbedLiveFile("orders.vuka.html", []byte(quiet)); out != nil {
		t.Errorf("a helper's hook and method made the page live (%q):\n%s", why, out)
	}
}
