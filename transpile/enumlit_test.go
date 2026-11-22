package transpile_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

const enumLitSrc = `package main

import (
	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/vuka/v2"
	design "golden/enumdesign"
)

type Variant string

const (
	VariantDefault Variant = ""
	VariantOutline Variant = "outline"
	VariantGhost   Variant = "ghost"
)

type Cols int

const (
	ColsOne Cols = 1
	ColsTwo Cols = 2
	ColsNeg Cols = -1
)

// Accent is a colour: a few presets, any other CSS colour too.
//
//vuka:open
type Accent string

const AccentBrand Accent = "brand"

// Label has no constants: any value.
type Label string

type ButtonProps struct {
	Variant  Variant
	Cols     Cols
	Accent   Accent
	Label    Label
	Icon     string
	Tone     design.Tone
	Target   design.Selector
	Gap      design.Gap
	Width    design.Width
	Maybe    vuka.Option[Variant]
	Children ui.Node
}

func Button(p ButtonProps) ui.Node { return <button>{p.Children}</button> }

func Page(v Variant, n int) ui.Node {
	return <div>
		@@
	</div>
}
`

func enumLitErrs(t *testing.T, markup string) transpile.ErrorList {
	t.Helper()
	src := strings.Replace(enumLitSrc, "@@", markup, 1)
	_, err := transpile.Package([]transpile.File{{Name: "page.vuka", Src: []byte(src)}},
		transpile.Options{Importer: load.NewImporter("testdata/golden", ""), Dir: "testdata/golden"})
	if err == nil {
		return nil
	}
	var list transpile.ErrorList
	if !errors.As(err, &list) {
		t.Fatalf("%s: %v", markup, err)
	}
	return list
}

// TestEnumLiterals: a quoted value set on a prop of a defined string type,
// or a number on one of an integer type, is one of the values of the
// constants its package declares of it; a hole is never checked, nor a type
// declaring no constants, nor one marked //vuka:open.
func TestEnumLiterals(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs the go command")
	}
	for _, ok := range []string{
		`<Button variant="outline">x</Button>`,
		`<Button variant="">x</Button>`,
		`<Button variant={v}>x</Button>`,
		`<Button variant={Variant("outlien")}>x</Button>`,
		`<Button variant={"anything"}>x</Button>`,
		`<Button cols={2}>x</Button>`,
		`<Button cols={-1}>x</Button>`,
		`<Button cols={Cols(n)}>x</Button>`,
		`<Button accent="#ff0000">x</Button>`,
		`<Button label="anything">x</Button>`,
		`<Button icon="delete-02">x</Button>`,
		`<Button tone="danger">x</Button>`,
		`<Button target="#cart">x</Button>`,
		`<Button gap="2xl">x</Button>`,
		`<Button width={2}>x</Button>`,
		`<Button maybe="ghost">x</Button>`,
	} {
		if errs := enumLitErrs(t, ok); len(errs) > 0 {
			t.Errorf("%s: %v", ok, errs)
		}
	}
	for _, c := range []struct{ markup, msg, suggest string }{
		{`<Button variant="outlien">x</Button>`, `<Button variant="outlien">: "outlien" is no Variant`, `"outline"`},
		{`<Button variant="VariantOutline">x</Button>`, `"VariantOutline" is no Variant`, `"outline"`},
		{`<Button variant="Ghost">x</Button>`, `"Ghost" is no Variant`, `"ghost"`},
		{`<Button variant="primary">x</Button>`, `"primary" is no Variant`, ``},
		{`<Button cols={3}>x</Button>`, `<Button cols={3}>: 3 is no Cols`, ``},
		{`<Button tone="dangr">x</Button>`, `"dangr" is no design.Tone`, `"danger"`},
		{`<Button width={3}>x</Button>`, `3 is no design.Width`, ``},
		{`<Button maybe="gost">x</Button>`, `"gost" is no Variant`, `"ghost"`},
	} {
		errs := enumLitErrs(t, c.markup)
		if len(errs) != 1 {
			t.Errorf("%s: want one error, got %v", c.markup, errs)
			continue
		}
		e := errs[0]
		if e.Code != "E0196" || !strings.Contains(e.Msg, c.msg) || e.Suggest != c.suggest {
			t.Errorf("%s: got %s %q (did you mean %q), want E0196 %q (%q)", c.markup, e.Code, e.Msg, e.Suggest, c.msg, c.suggest)
		}
		if !strings.Contains(e.Help, "takes the value of one of its constants") {
			t.Errorf("%s: help %q", c.markup, e.Help)
		}
	}
	errs := enumLitErrs(t, `<Button variant="outlien">x</Button>`)
	if len(errs) == 1 {
		if p := errs[0].Pos; p.Line != 53 || p.Column != 19 || errs[0].Len != len(`"outlien"`) {
			t.Errorf("at %d:%d len %d, want 53:19 len 9", p.Line, p.Column, errs[0].Len)
		}
	}
}
