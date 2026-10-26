package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
)

type User struct {
	ID   int
	Name string
}

type Store struct{}

func (s Store) Self() (Store, error) { return s, nil }
func (Store) Users() ([]User, error) { return []User{{1, "ann"}, {2, "bob"}}, nil }
func (Store) Title() (string, error) { return "Users", nil }
func (Store) Admin() (bool, error)   { return true, nil }
func (Store) Count() (int, error)    { return 2, nil }
func (Store) Class() (string, error) { return "list", nil }

func main() {
	out, err := ui.String(context.Background(), Page{})
	fmt.Println(out, err)
}
