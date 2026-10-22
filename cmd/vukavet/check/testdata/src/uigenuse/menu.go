package uigenuse

import (
	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/ui/v2/uigen"
)

type Menu struct {
	n uigen.Assign[int] // want `W0105: uigen.Assign is what the compiler writes`
}

func toggle(open any) ui.Commands {
	return uigen.Toggle(nil, open) // want `W0105: uigen.Toggle is what the compiler writes`
}
