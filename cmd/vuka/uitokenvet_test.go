package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestVetTokens: W0601 is a vendored component using a design token no
// stylesheet of the build defines — var(--x), a colour utility, Tailwind's
// bg-(--x) — naming the stylesheet expected to define it (the tokens', or
// a library's theme for a token of its own) and the nearest token defined;
// vuka's components and a library's, as vuka ui add writes them, have
// none, nor do Tailwind's own colours, names in comments, or a word that
// isn't in a class list.
func TestVetTokens(t *testing.T) {
	wd := widgetsApp(t)
	mustUI(t, wd, "add", "widgets/dialog", "widgets/select", "widgets/button")
	if ds := vetTokens(wd); len(ds) > 0 {
		t.Errorf("a library's components as vuka ui add writes them: %v", ds)
	}
	button := filepath.Join(wd, "components/ui/button/button.templ")
	writeFile(t, button, strings.Replace(read(t, button), `"wd-button", `, `"wd-button bg-sidebar-acent hover:text-primary", `, 1))
	ds := vetTokens(wd)
	if len(ds) != 1 || ds[0].Code != "W0601" || ds[0].Span.File != "components/ui/button/button.templ" ||
		ds[0].Message != "button uses bg-sidebar-acent, which no stylesheet of the build defines: styles/widgets.css (widgets's theme) was expected to define --color-sidebar-acent" {
		t.Errorf("a library's token: %v", ds)
	}

	dir := uiApp(t)
	mustUI(t, dir, "init")
	mustUI(t, dir, "add", "button", "badge", "card", "alert", "input", "label", "tabs", "dialog", "skeleton")
	if ds := vetTokens(dir); len(ds) > 0 {
		t.Errorf("vuka's components: %v", ds)
	}
	writeFile(t, filepath.Join(dir, "components/ui/chip.vuka.html"), `<script lang="go">
package design

// Chip is drawn with bg-nothing in this comment.
func chipIcon() string { return "text-search" }
</script>

<span class="inline-flex bg-rnig text-red-500 border-(--brnd) ring-ring/50 dark:bg-white" style="color: var(--muted-foregrund); outline-color: var(--tw-ring-color)">{Children}</span>
`)
	ds = vetTokens(dir)
	got := map[string]string{}
	for _, d := range ds {
		if d.Code != "W0601" || d.Span.File != "components/ui/chip.vuka.html" || d.Span.Line != 8 {
			t.Errorf("%+v", d)
		}
		got[d.Message] = d.Suggest
	}
	for msg, suggest := range map[string]string{
		"chip uses bg-rnig, which no stylesheet of the build defines: styles/app.css (the design tokens) was expected to define --color-rnig":                "bg-ring",
		"chip uses --brnd, which no stylesheet of the build defines: styles/app.css (the design tokens) was expected to define --brnd":                       "",
		"chip uses --muted-foregrund, which no stylesheet of the build defines: styles/app.css (the design tokens) was expected to define --muted-foregrund": "--muted-foreground",
	} {
		if s, ok := got[msg]; !ok || s != suggest {
			t.Errorf("no %q (did you mean %q); got %v", msg, suggest, got)
		}
	}
	if len(ds) != 3 {
		t.Errorf("%d diagnostics: %v", len(ds), got)
	}
	for _, d := range ds {
		if strings.Contains(d.Message, "bg-rnig") && (d.Span.Col != len(`<span class="inline-flex `)+1 || d.Help != "define --color-rnig in styles/app.css, or use a token it defines") {
			t.Errorf("bg-rnig: %+v", d)
		}
	}
}
