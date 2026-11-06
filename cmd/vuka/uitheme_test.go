package main

import (
	"maps"
	"path/filepath"
	"strings"
	"testing"
)

// tokenValue is the value css's tokens give name in the block opened by
// selector (its first).
func tokenValue(t *testing.T, css, selector, name string) string {
	t.Helper()
	_, rest, ok := strings.Cut(css, selector+" {\n")
	if !ok {
		t.Fatalf("no %s block:\n%s", selector, css)
	}
	block, _, _ := strings.Cut(rest, "}")
	_, v, ok := strings.Cut(block, name+": ")
	if !ok {
		t.Fatalf("%s hasn't %s:\n%s", selector, name, block)
	}
	v, _, _ = strings.Cut(v, ";")
	return v
}

// TestUITheme: vuka ui init --theme writes the tokens with a theme's
// colours, light and dark, recorded in [ui]; vuka ui theme switches to
// another, changing only the tokens between vuka ui's markers and keeping
// a token the app gave its own value; it lists the themes, says which
// name it doesn't know, and writes nothing with --dry-run.
func TestUITheme(t *testing.T) {
	dir := uiApp(t)
	out := mustUI(t, dir, "init", "--theme", "zinc")
	if !strings.Contains(out, "wrote styles/app.css: the zinc theme's colours") {
		t.Errorf("init --theme zinc:\n%s", out)
	}
	file := filepath.Join(dir, "styles/app.css")
	css := read(t, file)
	for _, c := range []struct{ selector, name, want string }{
		{":root", "--foreground", "oklch(0.141 0.005 285.823)"},
		{":root", "--radius", "0.625rem"},
		{`:root:not([data-theme="light"])`, "--background", "oklch(0.141 0.005 285.823)"},
		{`:root[data-theme="dark"]`, "--secondary", "oklch(0.274 0.006 286.033)"},
		{`:root[data-theme="dark"]`, "--skeleton-highlight", "oklch(0.371 0 0)"},
	} {
		if got := tokenValue(t, css, c.selector, c.name); got != c.want {
			t.Errorf("%s %s: %s, want %s", c.selector, c.name, got, c.want)
		}
	}
	if toml := read(t, filepath.Join(dir, "vuka.toml")); !strings.Contains(toml, "css = \"styles/app.css\"\ntheme = \"zinc\"\n") {
		t.Errorf("vuka.toml:\n%s", toml)
	}

	css = strings.Replace(css, "--primary: oklch(0.21 0.006 285.885);", "--primary: oklch(0.6 0.2 30);", 1)
	writeFile(t, file, css+"\n:root {\n  --ring: red;\n}\n")
	out = mustUI(t, dir, "theme", "slate")
	for _, want := range []string{"kept the app's --primary (its value isn't the theme's it replaces)", "wrote styles/app.css: the slate theme's colours", "wrote vuka.toml: [ui] theme"} {
		if !strings.Contains(out, want) {
			t.Errorf("theme slate doesn't say %q:\n%s", want, out)
		}
	}
	got := read(t, file)
	if tokenValue(t, got, ":root", "--primary") != "oklch(0.6 0.2 30)" || tokenValue(t, got, ":root", "--foreground") != "oklch(0.129 0.042 264.695)" ||
		tokenValue(t, got, `:root[data-theme="dark"]`, "--primary") != "oklch(0.929 0.013 255.508)" {
		t.Errorf("theme slate:\n%s", got)
	}
	if !strings.HasSuffix(got, tokensEnd+"\n\n/* The theme. */\n@theme {\n  --color-brand-500: #e8662c;\n}\n\n:root {\n  --ring: red;\n}\n") {
		t.Errorf("theme slate changed the stylesheet outside the tokens:\n%s", got)
	}
	if toml := read(t, filepath.Join(dir, "vuka.toml")); !strings.Contains(toml, "theme = \"slate\"\n") || strings.Contains(toml, "zinc") {
		t.Errorf("vuka.toml:\n%s", toml)
	}

	if out := mustUI(t, dir, "theme"); out != "the themes: blue, gray, neutral, slate, stone, zinc (the app's: slate)\n" {
		t.Errorf("theme:\n%s", out)
	}
	if _, err := runUI(t, dir, "theme", "slat"); err == nil || !strings.Contains(err.Error(), `no theme "slat" (did you mean slate?); the themes are blue, gray`) {
		t.Errorf("theme slat: %v", err)
	}
	before := snapshot(t, dir)
	if out := mustUI(t, dir, "theme", "--dry-run", "blue"); !strings.Contains(out, "would write styles/app.css: the blue theme's colours") {
		t.Errorf("theme --dry-run blue:\n%s", out)
	}
	if after := snapshot(t, dir); !maps.Equal(before, after) {
		t.Errorf("theme --dry-run changed the app")
	}
	if out := mustUI(t, dir, "theme", "neutral"); !strings.Contains(out, "kept the app's --primary") {
		t.Errorf("theme neutral:\n%s", out)
	}
	if got := read(t, file); tokenValue(t, got, ":root", "--foreground") != "oklch(0.145 0 0)" {
		t.Errorf("theme neutral:\n%s", got)
	}

	if _, err := runUI(t, uiApp(t), "theme", "zinc"); err == nil || !strings.Contains(err.Error(), "vuka.toml has no [ui]: run vuka ui init first") {
		t.Errorf("theme without [ui]: %v", err)
	}
}
