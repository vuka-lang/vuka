package main

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestUIForwarding: W0602 is a component that takes Attrs and spreads them
// on no element, or on another than the one its events are on, or takes
// Class and doesn't merge it last; the registry's components have none,
// and vuka vet checks the app's vendored ones.
func TestUIForwarding(t *testing.T) {
	files, _ := fs.Glob(registryFS, "registry/ui/*.vuka.html")
	for _, f := range files {
		src, _ := registryFS.ReadFile(f)
		for _, d := range forwardingDiags(strings.TrimPrefix(f, "registry/"), src) {
			t.Errorf("the registry's %s: %s", d.Span.File, d.Message)
		}
	}
	const head = "<script lang=\"go\">\npackage design\n\nimport \"github.com/vuka-lang/ui/v2\"\n\ntype Chip struct {\n\tClass    string\n\tAttrs    []ui.Attr\n\tOn       any\n\tChildren ui.Node\n}\n</script>\n\n"
	for markup, want := range map[string]string{
		`<span class={cn("chip", Class)} {Attrs...}>{Children}</span>`:                                      "",
		`<span class={cn("chip", Class)}>{Children}</span>`:                                                 "Chip declares Attrs and spreads them on no element",
		`<span class={cn("chip", Class)} {Attrs...}><button onClick={ui.On(On)}>{Children}</button></span>`: "Chip spreads Attrs on <span>, not on <button>, the element its events (onClick) are on",
		`<span class={cn(Class, "chip")} {Attrs...}>{Children}</span>`:                                      "Chip declares Class and doesn't merge it last",
		`<span class={chip.Class("tone", "a", "class", Class)} {Attrs...}>{Children}</span>`:                "",
	} {
		ds := forwardingDiags("components/ui/chip.vuka.html", []byte(head+markup+"\n"))
		switch {
		case want == "" && len(ds) > 0:
			t.Errorf("%s: %v", markup, ds)
		case want != "" && (len(ds) != 1 || ds[0].Code != "W0602" || !strings.HasPrefix(ds[0].Message, want)):
			t.Errorf("%s: %v, want %q", markup, ds, want)
		}
	}

	dir := uiApp(t)
	mustUI(t, dir, "init")
	mustUI(t, dir, "add", "badge")
	writeFile(t, filepath.Join(dir, "components/ui/badge.vuka.html"), strings.Replace(read(t, filepath.Join(dir, "components/ui/badge.vuka.html")), " {Attrs...}>", ">", 1))
	if ds := vetForwarding(dir); len(ds) != 1 || ds[0].Span.File != "components/ui/badge.vuka.html" || ds[0].Span.Line == 0 {
		t.Errorf("vet: %v", ds)
	}
}
