// Package widget stands in for a templ component library (templUI,
// shadcn-templ): props structs with an Attributes field, ids minted at
// random, context-scoped helpers, and a script that moves popups to the body.
package widget

import (
	"context"
	"crypto/rand"
	_ "embed"

	"github.com/a-h/templ"
)

func randomID() string { return "id-" + rand.Text() }

//go:embed widget.js
var script string

// Scripts is the library's script.
func Scripts() templ.Component { return templ.Raw("<script>" + script + "</script>") }

type dialogKey struct{}

// Trigger is the attributes that make an element open the Dialog around it.
func Trigger(ctx context.Context) templ.Attributes {
	id, _ := ctx.Value(dialogKey{}).(string)
	return templ.Attributes{"data-dialog-trigger": id, "aria-haspopup": "dialog", "aria-expanded": "false"}
}

// DialogID is the id of the Dialog around ctx's render.
func DialogID(ctx context.Context) string {
	id, _ := ctx.Value(dialogKey{}).(string)
	return id
}

type selectKey struct{}

type selectState struct{ id, value string }

func selectOf(ctx context.Context) selectState {
	s, _ := ctx.Value(selectKey{}).(selectState)
	return s
}
