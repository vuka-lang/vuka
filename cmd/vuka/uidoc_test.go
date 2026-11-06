package main

import (
	"strings"
	"testing"
)

// TestUIListComponent: vuka ui list <component> prints what a registry's
// component takes — its attributes, the values of its variants, its docs,
// slots and events — and <library>/<component> a library's components,
// read from its source; a name it doesn't know gets a suggestion, and a
// library's name still lists the library.
func TestUIListComponent(t *testing.T) {
	dir := widgetsApp(t)
	out := mustUI(t, dir, "list", "button")
	for _, want := range []string{
		"Button: A button: six variants",
		"<design.Button>  component, button.vuka.html:",
		`  variant   ButtonVariant  design.ButtonDefault (""), design.ButtonDestructive ("destructive"), design.ButtonOutline ("outline")`,
		"  on        any            event\n",
		"  on: On is what a click does: a ui.JS chain, or a live handler.\n",
		"  slots: children\n  events: on, onClick\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("list button lacks %q:\n%s", want, out)
		}
	}
	if out := mustUI(t, dir, "list", "dialog"); !strings.Contains(out, "  show         ui.State[bool]\n") || !strings.Contains(out, "  slots: children, <slot:footer>\n  events: onClose\n") {
		t.Errorf("list dialog:\n%s", out)
	}
	if out := mustUI(t, dir, "list", "tabs"); !strings.Contains(out, "<design.Tabs>  component") || !strings.Contains(out, "  value    ui.State[string]") || !strings.Contains(out, "  slots: <slot:tab>\n") {
		t.Errorf("list tabs:\n%s", out)
	}

	out = mustUI(t, dir, "list", "widgets/button")
	for _, want := range []string{"widgets/button, from widgets\n", "<button.Button>  function, button.templ:", `  variant     Variant           button.VariantDefault ("default"), button.VariantOutline ("outline")`, "  slots: children\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("list widgets/button lacks %q:\n%s", want, out)
		}
	}
	if out := mustUI(t, dir, "list", "widgets"); !strings.Contains(out, "widgets/dialog\n") {
		t.Errorf("list widgets:\n%s", out)
	}
	for arg, want := range map[string]string{
		"buton":         `no component "buton" (did you mean button?)`,
		"widgets/buton": `widgets has no component "buton" (did you mean button?)`,
		"widget/button": `no library "widget" (did you mean widgets?)`,
	} {
		if _, err := runUI(t, dir, "list", arg); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("list %s: %v, want %q", arg, err, want)
		}
	}
}
