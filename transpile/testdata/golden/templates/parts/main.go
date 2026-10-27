package main

import (
	"context"
	"errors"
	"fmt"

	ui "vukatest.local/stateui"
)

type Figures struct{ Open, Shortlisted int }

type Store struct{ down bool }

func (s Store) Stats(ctx context.Context) (Figures, error) { return Figures{3, 1}, nil }
func (s Store) Recent(ctx context.Context) ([]string, error) {
	if s.down {
		return nil, errors.New("down")
	}
	return []string{"a", "b"}, nil
}

func main() {
	for _, p := range []*Page{{}, {Store: Store{down: true}}} {
		out, err := ui.String(context.Background(), p)
		fmt.Println(out, err)
	}
}
