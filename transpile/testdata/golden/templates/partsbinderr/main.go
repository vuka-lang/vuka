package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
)

type Category struct{ Name string }

type Store struct{ calls map[string]int }

func (s Store) Categories(ctx context.Context) ([]Category, error) {
	s.calls["categories"]++
	return []Category{{"a"}, {"b"}}, nil
}

func (s Store) Unused(ctx context.Context) (int, error) {
	s.calls["unused"]++
	return 1, nil
}

func main() {
	s := Store{calls: map[string]int{}}
	out, err := ui.String(context.Background(), &Page{Store: s})
	fmt.Println(out, err)
	fmt.Println("calls:", s.calls["categories"], s.calls["unused"])
}
