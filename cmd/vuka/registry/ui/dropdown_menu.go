package design

import "github.com/vuka-lang/ui/v2"

// DropdownItem is an item of a DropdownMenu, a <slot:item>: what a click
// does, and the slot's content as its label.
type DropdownItem struct {
	// OnClick is what a click does: statements, a ui.JS chain or a live
	// handler, onClick={p.Edit}.
	OnClick     any
	Destructive bool
	Disabled    bool
	Label       ui.Node
}
