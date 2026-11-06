package pages

import (
	"variants"

	"github.com/vuka-lang/ui/v2"
)

type Index struct{}

func (Index) View() ui.Node {
	a := &variants.Alert{Tone: "warning"} // want `E0371: variants.Alert's Tone is its variant tone, which has no value "warning": it is "error" or "info"`
	return ui.HTML(`{a}`, a)
}
