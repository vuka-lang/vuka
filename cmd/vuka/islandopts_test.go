package main

import (
	"path/filepath"
	"strings"
	"testing"
)

const islandOptsSource = `package pages

import (
	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"

	"example.com/app/layouts"
)

@web.Live("/levels")
@ui.Island(ui.Layout(layouts.Admin("Levels (all)"), layouts.Shell))
type Levels struct {
	ui.Live
	Q string
}

@web.Get("/old")
@ui.Island("islands/Old.vue", ui.Strategy("idle"), ui.Layout(
	layouts.Page("Old"),
))
type Old struct {
	ui.Live
}

@web.Get("/stateless")
@ui.Island("", "load")
type Stateless struct {
	Q string
}

@ui.Island(ui.Strategy("media"))
type Widget struct {
	Live ui.Live
}
`

func TestReadIslandOptions(t *testing.T) {
	pa := &pageAssets{root: "/m"}
	if errs := pa.readIslands("/m/pages/p.vuka", []byte(islandOptsSource)); len(errs) > 0 {
		t.Fatal(errs)
	}
	got := map[string]*islandComp{}
	for _, ic := range pa.islands {
		got[ic.name] = ic
	}
	for _, c := range []struct {
		name, src, strategy, route string
		infer, layout, live        bool
	}{
		{"Levels", "", "", "Live", true, true, true},
		{"Old", "islands/Old.vue", "idle", "Get", false, true, true},
		{"Stateless", "", "load", "Get", true, false, false},
		{"Widget", "", "media", "", true, false, false},
	} {
		ic := got[c.name]
		if ic == nil {
			t.Fatalf("%s: not read (%d islands)", c.name, len(pa.islands))
		}
		if ic.infer != c.infer || c.src != "" && ic.rel != "pages/"+c.src || ic.strategy != c.strategy || ic.route != c.route || ic.layout != c.layout || ic.live != c.live {
			t.Errorf("%s: %+v", c.name, ic)
		}
	}
	if a := got["Old"].routeAdvice(); !strings.Contains(a, "Old embeds ui.Live: route it with @web.Live") || got["Old"].routeLabel != "pages/p.vuka:17" {
		t.Errorf("advice %q at %s", a, got["Old"].routeLabel)
	}
	for _, n := range []string{"Levels", "Stateless", "Widget"} {
		if a := got[n].routeAdvice(); a != "" {
			t.Errorf("%s: %s", n, a)
		}
	}
}

func TestReadIslandOptionErrors(t *testing.T) {
	for decl, want := range map[string]string{
		`@ui.Island(paths.Chart)`:                  "takes a string literal path",
		`@ui.Island(ui.Strategy(s))`:               "ui.Strategy takes a string literal",
		`@ui.Island("a.vue", ui.Layout(x), other)`: "options are calls",
	} {
		src := "package p\n\nimport \"github.com/vuka-lang/ui\"\n\n" + decl + "\ntype T struct{}\n"
		pa := &pageAssets{root: "/m"}
		errs := pa.readIslands("/m/p.vuka", []byte(src))
		if len(errs) != 1 || !strings.Contains(errs[0].Error(), want) {
			t.Errorf("%s: %v", decl, errs)
		}
	}
}

func TestLSPIslandRouteAdvice(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"pages/p.vuka": islandOptsSource,
		"islands/Levels.vue": "<template><p/></template>\n", "pages/islands/Old.vue": "<template><p/></template>\n",
		"islands/Stateless.vue": "<template><p/></template>\n", "islands/Widget.vue": "<template><p/></template>\n"})
	uri := pathToURI(filepath.Join(dir, "pages/p.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": islandOptsSource}})
	var diags string
	c.waitDiags(uri, func(ds []any) bool {
		diags = jsonOf(ds)
		return strings.Contains(diags, "route it with @web.Live")
	})
	if strings.Count(diags, `"message"`) != 1 || !strings.Contains(diags, `"severity":2`) || !strings.Contains(diags, `"line":16`) {
		t.Errorf("diagnostics: %s", diags)
	}
	at := func(needle string) map[string]any {
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": positionOf([]byte(islandOptsSource), strings.Index(islandOptsSource, needle)+4)}
	}
	if h := jsonOf(c.call("textDocument/hover", at("@ui.Island(ui.Layout"))); !containsAll(h, []string{"island `Levels`", "placed inside another page, the island ignores it"}) {
		t.Errorf("hover: %s", h)
	}
	if h := jsonOf(c.call("textDocument/hover", at("@ui.Island(\"\", \"load\")"))); strings.Contains(h, "ui.Layout") {
		t.Errorf("hover without a layout: %s", h)
	}
}
