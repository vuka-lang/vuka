package transpile_test

import (
	"github.com/vuka-lang/vuka/v2/transpile"
	"reflect"
	"testing"
)

func TestTemplateRefs(t *testing.T) {
	got := transpile.TemplateRefs(`<main><dialog ref="dialog"></dialog>
 if open { <input ref="search"/> }
 for _, x := range xs { <input ref="item" value={x}/> }
 <slot:body><button ref="close"/></slot:body>
 switch mode {
 case "edit":
  <textarea ref="editor"/>
 default:
  <select ref="choice"/>
 }
</main>`)
	want := []transpile.TemplateRef{
		{Name: "dialog", Tag: "dialog"},
		{Name: "search", Tag: "input", Conditional: true},
		{Name: "item", Tag: "input", Loop: true},
		{Name: "close", Tag: "button", Conditional: true},
		{Name: "editor", Tag: "textarea", Conditional: true},
		{Name: "choice", Tag: "select", Conditional: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("refs: %#v, want %#v", got, want)
	}
}

func TestTemplateRefsIncomplete(t *testing.T) {
	for _, markup := range []string{`<input ref="search"`, `<main>
if open {
<input ref="search"/>
`} {
		if got := transpile.TemplateRefs(markup); got != nil {
			t.Fatalf("incomplete markup refs: %#v", got)
		}
	}
}
