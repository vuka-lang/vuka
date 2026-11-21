package main

import (
	"os"
	"path/filepath"
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

// reflectMethodCallers are the functions, reachable in vuka new's app, that
// may call reflect's Method or MethodByName with a name that isn't a
// constant. One is enough for the linker to keep every exported method of
// every type converted to an interface: dead-method elimination is off for
// the whole binary. vuka's libraries find methods in the tables Vuka
// generates (vuka.Methods), so there is none; keep it so.
var reflectMethodCallers []string

// TestReleaseLink builds vuka new's app with vuka build and reads what the
// linker kept (-ldflags=-dumpdep): no development code, and no new caller
// of reflect's Method or MethodByName with a computed name.
func TestReleaseLink(t *testing.T) {
	e := newE2E(t)
	dir := e.new("lean")
	if _, err := tailwindBinary(dir); err != nil {
		needs(t, "no Tailwind CLI: vuka build needs it")
	}
	// An exported method nothing calls, of a type converted to an
	// interface: the linker drops it only with dead-method elimination on.
	if err := os.WriteFile(filepath.Join(dir, "dmeprobe.go"), []byte(dmeProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "lean")
	g := readDumpdep(e.run(dir, nil, "vuka", "build", "-ldflags=-dumpdep", "-o", bin, "."))
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
		if !ok || strings.HasPrefix(p, "type:") {
			continue // kept as a method of a type: what a caller keeps
		}
		seen[s] = true
		if !slices.Contains(reflectMethodCallers, s) {
			t.Errorf("%s calls reflect's Method or MethodByName with a computed name, and vuka build links it:\n%s\n"+
				"with one such call the linker keeps every exported method of every type converted to an interface.\n"+
				"Look the method up by a constant name, through an interface, or in Vuka's method tables (vuka.Methods).",
				s, linkPath(from, s))
		}
	}
	for _, s := range reflectMethodCallers {
		if !seen[s] {
			t.Logf("%s no longer calls reflect's Method or MethodByName in vuka new's app: take it off reflectMethodCallers", s)
		}
	}
	if len(reflectMethodCallers) == 0 && strings.Contains(e.run(dir, nil, "go", "tool", "nm", bin), "DMEProbeUnused") {
		t.Error("vuka build keeps an exported method nothing calls (main.dmeProbe.DMEProbeUnused): dead-method elimination is off")
	}
}

const dmeProbe = `package main

import (
	"fmt"
	"os"
)

type dmeProbe struct{ N int }

func (dmeProbe) DMEProbeUnused() string { return "kept only when dead-method elimination is off" }

func init() {
	if os.Getenv("VUKA_DME_PROBE") == "print" {
		fmt.Println(any(dmeProbe{}))
	}
}
`

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
