// Package di wires @di.Component structs into nexus's container: each one is
// provided through its constructor, which takes the struct's dependency fields.
package di

import (
	"github.com/paulmanoni/nexus/v2"
	"github.com/vuka-lang/vuka"
)

var providers []any

// Component is a type decorator: @di.Component on a struct.
func Component(t *vuka.Type) { providers = append(providers, t.New) }

// Module provides every component to the app.
func Module() nexus.Option { return nexus.Provide(providers...) }
