package main

import ui "vukatest.local/stateui"

var _ ui.Node

type tally struct{ N int }

func (t *tally) Inc() int { t.N++; return t.N }

func main() {}
