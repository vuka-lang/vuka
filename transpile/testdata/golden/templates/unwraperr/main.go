package main

import ui "vukatest.local/stateui"

var _ ui.Node

type User struct{ Name string }

type Store struct{}

func (Store) Find(name string) (User, bool) { return User{name}, true }
func (Store) Page() ([]User, int, error)    { return nil, 0, nil }
func (Store) Save() error                   { return nil }

func main() {}
