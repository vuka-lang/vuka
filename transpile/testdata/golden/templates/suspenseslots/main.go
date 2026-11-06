package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
)

// Store counts its calls.
type Store struct{ calls map[string]int }

func (s Store) Count(ctx context.Context) (int, error) {
	s.calls["count"]++
	return 3, nil
}

func (s Store) Items(ctx context.Context) ([]string, error) {
	s.calls["items"]++
	return []string{"a", "b"}, nil
}

func NewPtrPage(s Store) *PtrPage { return &PtrPage{Store: s} }

func main() {
	s := Store{calls: map[string]int{}}
	out, err := ui.String(context.Background(), NewPtrPage(s))
	fmt.Println(out, err)
	out, err = ui.String(context.Background(), ValPage{Store: s})
	fmt.Println(out, err)
}
