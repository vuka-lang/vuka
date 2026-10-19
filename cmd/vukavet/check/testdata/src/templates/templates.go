package templates

import (
	"context"
	"errors"
	"io"

	"github.com/a-h/templ"
	"github.com/vuka-lang/ui"

	"templates/tpl"
)

var row = // want row:"8 holes"
ui.Template(`
<li key="{{0}}" class="item {{1}}" {{2}}>
  <a href="/orders/{{3}}" onClick="{{4}}">{{5}}</a>
  <title>{{6}}</title>
  <input value="{{7}}"/>
</li>`)

type Order struct {
	ID   int
	Ref  string
	Late bool
}

func (o Order) String() string { return o.Ref }

type amount float32

type comp struct{}

func (comp) Render(ctx context.Context, w io.Writer) error { return nil }

func render(o Order, n ui.Node, v any) {
	row.Render(o.ID, "late", ui.Attr{Name: "id", Value: "x"}, o.ID, func() {}, n, "t", 3)
	row.Render(o.ID, ui.Class{"late": o.Late}, []ui.Attr{}, o, ui.On(nil), []ui.Node{n}, errors.New("x"), v)
	row.Render(o.ID, "late", templ.Attributes{}, amount(2), nil, ui.Attr{}, comp{}, true) // want `E0128: \{\{5\}\} is a child of <a>; it is given ui.Attr, an attribute set` `E0128: \{\{6\}\} is text in <title>; it is given comp, not text`
	row.Render(o.ID, "late", "id=x", o.Late, "go", n, func() {}, ui.Class{})              // want `E0128: \{\{2\}\} is an attribute set of <li>; it is given string` `E0128: \{\{3\}\} is part of href on <a>; it is given bool, not text` `E0128: \{\{4\}\} is onClick on <a>; it is given string, not a handler` `E0128: \{\{6\}\} is text in <title>; it is given func\(\), not text` `E0128: \{\{7\}\} is value on <input>; it is given ui.Class, a class: only class takes one`
	row.Render(o.ID, "late")                                                              // want `E0127: row has 8 holes, and Render is given 2 values`
	tpl.Row.Render("a")                                                                   // want `E0127: tpl.Row has 2 holes, and Render is given 1 values`
	tpl.Row.Render(ui.Style{}, "b")                                                       // want `E0128: \{\{0\}\} is class on <tr>; it is given ui.Style, a style: only style takes one`
	ui.Template(`<p>{{0}}</p>`).Render()                                                  // want `E0127: ui.Template\(.*\) has 1 holes, and Render is given 0 values`
	local := ui.Template(`<p {{0}}></p>`)
	local.Render(1) // want `E0128: \{\{0\}\} is an attribute set of <p>; it is given int`
	args := []any{1}
	local.Render(args...)
}
