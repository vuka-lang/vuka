package main

import "github.com/vuka-lang/ui"

// Counter is a stateful component: its count is its own
// (counter.vuka.html).
type Counter struct {
	ui.Live
	Label string
	n     int
}

// Inc counts one.
func (c *Counter) Inc() { c.n++ }
