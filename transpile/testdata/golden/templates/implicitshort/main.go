package main

import (
	"context"

	ui "vukatest.local/stateui"
)

type CategoryService struct{}

type Category struct{ Name string }

type Store struct{}

func (Store) Categories(ctx context.Context, svc *CategoryService) ([]Category, error) {
	return nil, nil
}

func (Store) Format(n int, unit string) string { return "" }

func main() { _ = ui.HTML }
