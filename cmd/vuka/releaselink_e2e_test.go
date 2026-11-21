package main

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// devOnly are symbols only development needs, which vuka build must not
// link: web's toolbar (left out by the vuka_release tag) and text/template's
// executor, which came with it. A name ending in a dot is a prefix.
var devOnly = []string{
	"github.com/vuka-lang/web/v2.renderPanels",
	"github.com/vuka-lang/web/v2.(*App).routeCrons",
	"github.com/vuka-lang/web/v2.toolbarScript",
	"github.com/vuka-lang/web/v2.graphiqlScript",
	"text/template.(*state).",
}

// reflectMethodCallers are the functions of vuka's libraries, reachable in
// vuka new's app, that call reflect's Method or MethodByName with a name
// that isn't a constant. One is enough for the linker to keep every exported
// method of every type converted to an interface: dead-method elimination
// is off for the whole binary. This list is the debt; it only shrinks.
var reflectMethodCallers = []string{
	"github.com/vuka-lang/orm/v2.buildModel",
	"github.com/vuka-lang/ui/v2.(*formType).methods",
	"github.com/vuka-lang/ui/v2.CheckParams",
	"github.com/vuka-lang/ui/v2.CheckParts",
	"github.com/vuka-lang/ui/v2.IslandMethods",
	"github.com/vuka-lang/ui/v2.deferGroupsFor",
	"github.com/vuka-lang/ui/v2.init",
	"github.com/vuka-lang/ui/v2.namedHookErr",
	"github.com/vuka-lang/ui/v2.promoted",
	"github.com/vuka-lang/ui/v2/live.pushable",
	"github.com/vuka-lang/ui/v2/live.updateMethod",
	"github.com/vuka-lang/web/v2.patchParams",
	"github.com/vuka-lang/web/v2.results.func2.func1",
	"github.com/vuka-lang/web/v2/auth.lifecycle",
}

// TestReleaseLink builds vuka new's app with vuka build and reads what the
// linker kept (-ldflags=-dumpdep): no development code, and no new caller
// of reflect's Method or MethodByName with a computed name.
func TestReleaseLink(t *testing.T) {
	e := newE2E(t)
	dir := e.new("lean")
	if _, err := tailwindBinary(dir); err != nil {
		needs(t, "no Tailwind CLI: vuka build needs it")
	}
	g := readDumpdep(e.run(dir, nil, "vuka", "build", "-ldflags=-dumpdep", "-o", os.DevNull, "."))
	if len(g.edges) == 0 {
		t.Fatal("-dumpdep printed no edges")
	}
	from := g.reach()
	for _, sym := range devOnly {
		for s := range from {
			if s == sym || strings.HasSuffix(sym, ".") && strings.HasPrefix(s, sym) {
				t.Errorf("vuka build links %s, which only development needs:\n%s\n"+
					"keep it behind the vuka_release build tag (web's toolbar_serve.go), or off the path from main", s, linkPath(from, s))
				break
			}
		}
	}
	seen := map[string]bool{}
	for s := range g.reflectMethod {
		p, ok := from[s]
		if !ok || strings.HasPrefix(p, "type:") || !strings.HasPrefix(s, "github.com/vuka-lang/") && !strings.HasPrefix(s, "main.") {
			continue // kept as a method of a type, or the standard library's
		}
		seen[s] = true
		if !slices.Contains(reflectMethodCallers, s) {
			t.Errorf("%s calls reflect's Method or MethodByName with a computed name, and vuka build links it:\n%s\n"+
				"with one such call the linker keeps every exported method of every type converted to an interface.\n"+
				"Look the method up by a constant name, or through an interface or a table made at compile time.",
				s, linkPath(from, s))
		}
	}
	for _, s := range reflectMethodCallers {
		if !seen[s] {
			t.Logf("%s no longer calls reflect's Method or MethodByName in vuka new's app: take it off reflectMethodCallers", s)
		}
	}
}

type dumpdep struct {
	edges         map[string][]string
	reflectMethod map[string]bool // functions marked <ReflectMethod>
}

// readDumpdep reads the linker's -dumpdep output: "from -> to" lines, each
// symbol perhaps followed by attributes in angle brackets.
func readDumpdep(out string) dumpdep {
	g := dumpdep{edges: map[string][]string{}, reflectMethod: map[string]bool{}}
	sym := func(s string) string {
		for _, attr := range []string{" <ReflectMethod>", " <UsedInIface>"} {
			if name, ok := strings.CutSuffix(s, attr); ok {
				if attr == " <ReflectMethod>" {
					g.reflectMethod[name] = true
				}
				s = name
			}
		}
		return s
	}
	for line := range strings.SplitSeq(out, "\n") {
		a, b, ok := strings.Cut(line, " -> ")
		if !ok {
			continue
		}
		a = sym(a)
		g.edges[a] = append(g.edges[a], sym(b))
	}
	return g
}

// reach is what the program reaches from main and the packages' inits,
// each symbol with the one that first reached it.
func (g dumpdep) reach() map[string]string {
	from := map[string]string{}
	var queue []string
	for s := range g.edges {
		if s == "runtime.main" || s == "main.main" || strings.HasSuffix(s, "..inittask") {
			from[s] = ""
			queue = append(queue, s)
		}
	}
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		for _, to := range g.edges[s] {
			if _, ok := from[to]; !ok {
				from[to] = s
				queue = append(queue, to)
			}
		}
	}
	return from
}

func linkPath(from map[string]string, s string) string {
	var p []string
	for ; s != ""; s = from[s] {
		p = append(p, "    "+s)
	}
	slices.Reverse(p)
	return strings.Join(p, "\n")
}
