package transpile_test

import (
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

const sugarButton = `<script lang="go">
package main

import ui "vukatest.local/stateui"

type Tone string

const (
	ToneNeutral Tone = "neutral"
	ToneDanger  Tone = "danger"
)

type Button struct {
	Type     string ` + "`vuka:\"default=button\"`" + `
	Tone     Tone   ` + "`vuka:\"default=ToneNeutral\"`" + `
	Size     int    ` + "`vuka:\"default=2\"`" + `
	Wide     bool   ` + "`vuka:\"default=true\"`" + `
	Label    string
	Attrs    []ui.Attr
	Children ui.Node
}
</script>

<button type={Type} data-tone={string(Tone)} {Attrs...}>{Children}</button>
`

const sugarCard = `<script lang="go">
package main

import ui "vukatest.local/stateui"

type Card struct {
	Title    string
	Children ui.Node
}
</script>

<section>{Children}</section>
`

const sugarDialog = `<script lang="go">
package main

import ui "vukatest.local/stateui"

type Dialog struct {
	ID    ui.AutoID
	Title string
}
</script>

<dialog id={ID} aria-labelledby={ID.Suffix("title")}>
  <h2 id={ID.Suffix("title")}>{Title}</h2>
</dialog>
`

const sugarPage = `<script lang="go">
package main

import ui "vukatest.local/stateui"

type Rule struct{ Target string }

type Page struct {
	ID   string
	Hint string
	Tone Tone
	Kind string
	Open bool
	Rule Rule
	n    int
}
</script>

<main>
  @@
</main>
`

// compileSugar compiles markup in a page beside Button and Card, over a
// target with an Attr type of its own (opttarget); compileIDs over one with
// AutoID (stateui).
func compileSugar(t *testing.T, markup string, files ...string) (string, error) {
	t.Helper()
	opt := func(s string) string {
		return strings.ReplaceAll(s, `"vukatest.local/stateui"`, `"vukatest.local/opttarget"`)
	}
	fs := map[string]string{
		"page.vuka.html":   opt(strings.Replace(sugarPage, "@@", markup, 1)),
		"button.vuka.html": opt(sugarButton),
		"card.vuka.html":   opt(sugarCard),
	}
	for i := 0; i+1 < len(files); i += 2 {
		fs[files[i]] = opt(files[i+1])
	}
	return compileFiles(fs)
}

func compileIDs(t *testing.T, markup string) (string, error) {
	t.Helper()
	page := strings.Replace(sugarPage, "@@", markup, 1)
	return compileFiles(map[string]string{"page.vuka.html": page, "dialog.vuka.html": sugarDialog})
}

func compileFiles(fs map[string]string) (string, error) {
	res, err := compileShapes(fs)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, o := range res.Files {
		b.Write(o.Src)
	}
	return b.String(), nil
}

func wantIn(t *testing.T, src string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(src, w) {
			t.Errorf("no %s in\n%s", w, src)
		}
	}
}

// TestAttrPassthrough: attr:name on a component is an entry of its Attrs,
// with no field of that name looked for; on an element it is the attribute.
func TestAttrPassthrough(t *testing.T) {
	src, err := compileSugar(t, `<Button attr:form={"rule-" + Rule.Target} attr:data-x="lit" attr:label="html">Save</Button>
  <input attr:model="m" attr:className="c" attr:onclick="go()" />`)
	if err != nil {
		t.Fatal(codes(err))
	}
	wantIn(t, src,
		`Attrs: []ui.Attr{ui.Attr{Name: "form", Value: "rule-" + p.Rule.Target}, ui.Attr{Name: "data-x", Value: "lit"}, ui.Attr{Name: "label", Value: "html"}}`,
		`<input model="m" className="c" onclick="go()"/>`)
}

// TestAttrPassthroughNoAttrs: attr: on a component with no Attrs field is
// E0106.
func TestAttrPassthroughNoAttrs(t *testing.T) {
	_, err := compileSugar(t, `<Card attr:form="f">x</Card>`)
	if err == nil || !strings.Contains(codes(err), "E0106") || !strings.Contains(err.Error(), "no Attrs") {
		t.Fatalf("want E0106, got %v", err)
	}
}

// TestOmitZero: name?={v} on an element is ui.OmitZero(v); on a prop it
// is the prop when no default stands aside, cmp.Or with the default when
// one does; in Attrs it is an entry holding ui.OmitZero(v).
func TestOmitZero(t *testing.T) {
	src, err := compileSugar(t, `<input id?={ID} placeholder?={Hint} />
  <Card title?={Hint}>x</Card>
  <Button type?={Kind} tone?={Tone} attr:form?={ID} title?={Hint}>go</Button>`)
	if err != nil {
		t.Fatal(codes(err))
	}
	wantIn(t, src,
		`<input id={ui.OmitZero(p.ID)} placeholder={ui.OmitZero(p.Hint)}/>`,
		`Card{Title: p.Hint,`,
		`Type: cmp.Or[string](p.Kind, "button")`,
		`Tone: cmp.Or[Tone](p.Tone, "neutral")`,
		`ui.Attr{Name: "form", Value: ui.OmitZero(p.ID)}`,
		`ui.Attr{Name: "title", Value: ui.OmitZero(p.Hint)}`)
}

// TestOmitZeroErrors: ?= on a bool, a quoted value, an event or what
// isn't an attribute is E0135.
func TestOmitZeroErrors(t *testing.T) {
	for _, c := range []struct{ markup, want string }{
		{`<input disabled?={Open} />`, "is a bool"},
		{`<Button wide?={Open}>x</Button>`, "is a bool"},
		{`<input id?="x" />`, "takes an expression"},
		{`<button onClick?={nil}>x</button>`, "event's handler"},
		{`<li key?={ID}>x</li>`, "isn't an attribute"},
	} {
		_, err := compileSugar(t, c.markup)
		if err == nil || !strings.Contains(codes(err), "E0135") || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want E0135 %q, got %v", c.markup, c.want, err)
		}
	}
}

// TestPropDefaults: a prop the tag leaves out is its default; one it sets,
// even to the zero value, is what it says.
func TestPropDefaults(t *testing.T) {
	src, err := compileSugar(t, `<Button>a</Button>
  <Button type="" tone={ToneDanger} size={0} wide={false}>b</Button>`)
	if err != nil {
		t.Fatal(codes(err))
	}
	wantIn(t, src,
		`Type: "button", Tone: "neutral", Size: 2, Wide: true`,
		`Button{Type: "", Tone: ToneDanger, Size: 0, Wide: false,`)
}

// TestPropDefaultErrors: a default that can't be is E0136 at its field.
func TestPropDefaultErrors(t *testing.T) {
	for _, c := range []struct{ field, want string }{
		{"Size int `vuka:\"default=big\"`", "neither an integer nor a constant"},
		{"Size int `vuka:\"default=1.5\"`", "isn't an integer"},
		{"Wide bool `vuka:\"default=yes\"`", "neither a bool"},
		{"Tone Tone `vuka:\"default=ToneNeutrl\"`", "did you mean default=ToneNeutral"},
		{"Tone Tone `vuka:\"default=loud\"`", "none of Tone's constants"},
		{"Kind string `vuka:\"required,default=x\"`", "beside required"},
		{"Kids ui.Node `vuka:\"default=x\"`", "takes no default"},
		{"Small int8 `vuka:\"default=300\"`", "overflows"},
		{"Gap string `vuka:\"default=gapDefault\"`", "unexported constant"},
	} {
		badge := `<script lang="go">
package main

import ui "vukatest.local/stateui"

var _ ui.Node

const gapDefault = "md"

type Badge struct {
	` + c.field + `
}
</script>

<span>badge</span>
`
		_, err := compileSugar(t, `<p>x</p>`, "badge.vuka.html", badge)
		if err == nil || !strings.Contains(codes(err), "E0136") || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want E0136 %q, got %v", c.field, c.want, err)
			continue
		}
		if !strings.Contains(err.Error(), "badge.vuka.html:11:") {
			t.Errorf("%s: not at the field: %v", c.field, err)
		}
	}
}

// TestAutoID: a ui.AutoID prop a tag leaves out is ui.NewAutoID's, the tag
// in ui.Ctx; one it sets is its value; ?= takes the id when zero.
func TestAutoID(t *testing.T) {
	src, err := compileIDs(t, `<Dialog title="a" />
  <Dialog id="confirm" title="b" />
  <Dialog id?={ui.AutoID(ID)} title="c" />`)
	if err != nil {
		t.Fatal(codes(err))
	}
	wantIn(t, src,
		`ui.Ctx(func(ctx context.Context) ui.Node { return Dialog{Title: "a", ID: ui.NewAutoID(ctx)}`,
		`Dialog{ID: "confirm", Title: "b"}`,
		`ID: cmp.Or[ui.AutoID](ui.AutoID(p.ID), ui.NewAutoID(ctx))`)
}

// TestOmitZeroOldTarget: name?= on an element, or in a component's Attrs,
// needs a target with OmitZero.
func TestOmitZeroOldTarget(t *testing.T) {
	button := `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

type Button struct {
	Attrs    []ui.Attr
	Children ui.Node
}
</script>

<button {Attrs...}>{Children}</button>
`
	for _, markup := range []string{`<input id?={ID} />`, `<Button title?={Hint}>x</Button>`} {
		page := strings.Replace(strings.Replace(sugarPage, `import ui "vukatest.local/stateui"`, `import "github.com/vuka-lang/ui/v2"`, 1), "@@", markup, 1)
		page = strings.Replace(page, "\tTone Tone\n", "", 1)
		_, err := compileFiles(map[string]string{"page.vuka.html": page, "button.vuka.html": button})
		if err == nil || !strings.Contains(codes(err), "E0135") || !strings.Contains(err.Error(), "needs ui.OmitZero") {
			t.Errorf("%s: want E0135 needs ui.OmitZero, got %v", markup, err)
		}
	}
}

// TestPropDefaultOtherPackage: a default naming another package's
// constant is written as that constant, so a build sees its value change.
func TestPropDefaultOtherPackage(t *testing.T) {
	compile := func(markup string) (string, error) {
		src := strings.Replace(enumLitSrc, "@@", markup, 1)
		res, err := transpile.Package([]transpile.File{{Name: "page.vuka", Src: []byte(src)}},
			transpile.Options{Importer: load.NewImporter("testdata/golden", ""), Dir: "testdata/golden"})
		if err != nil {
			return "", err
		}
		return string(res.Files[0].Src), nil
	}
	src, err := compile(`<design.Badge />`)
	if err != nil {
		t.Fatal(codes(err))
	}
	wantIn(t, src, `design.BadgeProps{Tone: design.ToneDanger}`)
}
