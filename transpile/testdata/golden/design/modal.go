// Package design is a component library of another package: a parent's
// state handed to Modal's Show can't be followed, so it classifies synced.
package design

import (
	ui "vukatest.local/stateui"
)

// Modal shows its content while Show holds.
type Modal struct {
	ui.Live
	Show ui.State[bool]
}

func (m *Modal) Render() ui.Node {
	return ui.El("div", []ui.Attr{{Name: "class", Value: "modal"}})
}
