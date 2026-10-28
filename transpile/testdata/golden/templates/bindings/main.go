package main

import (
	"context"
	"errors"
	"fmt"

	ui "vukatest.local/stateui"
)

type Category struct{ Name string }

// Store counts its calls.
type Store struct{ calls map[string]int }

func (s Store) Categories(ctx context.Context) ([]Category, error) {
	s.calls["categories"]++
	return []Category{{"a"}, {"b"}}, nil
}

func (s Store) Unused(ctx context.Context) (int, error) {
	s.calls["unused"]++
	return 0, nil
}

func (s Store) Hidden(ctx context.Context) (int, error) {
	s.calls["hidden"]++
	return 0, nil
}

func (s Store) Broken(ctx context.Context) (string, error) {
	s.calls["broken"]++
	return "", errors.New("store down")
}

func render(p Page) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("render failed: %v", r)
		}
	}()
	return ui.String(context.Background(), p)
}

func main() {
	for _, fail := range []bool{false, true} {
		s := Store{calls: map[string]int{}}
		out, err := render(Page{Store: s, Fail: fail})
		fmt.Println(out, err)
		fmt.Println("calls:", s.calls["categories"], s.calls["unused"], s.calls["hidden"], s.calls["broken"])
	}
}
