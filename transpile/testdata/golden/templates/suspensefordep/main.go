package main

import (
	"context"
	"errors"
	"fmt"

	ui "vukatest.local/stateui"
)

type Category struct{ Name string }

type Stats struct{ Open int }

// Store counts its calls.
type Store struct{ calls map[string]int }

func (s Store) Categories(ctx context.Context) ([]Category, error) {
	s.calls["categories"]++
	return []Category{{"a"}, {"b"}}, nil
}

func (s Store) Stats(ctx context.Context) (Stats, error) {
	s.calls["stats"]++
	return Stats{3}, nil
}

func (s Store) Broken(ctx context.Context) (int, error) {
	s.calls["broken"]++
	return 0, errors.New("down")
}

func main() {
	s := Store{calls: map[string]int{}}
	out, err := ui.String(context.Background(), &Page{Store: s})
	fmt.Println(out, err)
	fmt.Println("calls:", s.calls["categories"], s.calls["stats"], s.calls["broken"])
}

func CategoryCount(cs []Category) int { return len(cs) }
