// Package uigen stands in for ui's: what the compiler's output calls.
package uigen

import "github.com/vuka-lang/ui/v2"

type Assign[T any] struct{ v T }

func (a *Assign[T]) Get() T { return a.v }

func Toggle(c ui.Commands, s any) ui.Commands { return c }
