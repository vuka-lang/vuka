package main

import ui "vukatest.local/stateui"

type Store struct{}

func (Store) Count() int { return 2 }

func main() { _ = ui.HTML }
