package main

import (
	"context"
	"fmt"
	"strings"

	ui "vukatest.local/stateui"
)

type ctxKey struct{}

type Category struct{ Name string }

type Store struct{}

func (Store) Categories(ctx context.Context) ([]Category, error) {
	return []Category{{ctx.Value(ctxKey{}).(string) + "-a"}, {"b"}}, nil
}

func (Store) Count(ctx context.Context) int { return 2 }

func Join(ctx context.Context, sep string, parts ...string) string {
	return ctx.Value(ctxKey{}).(string) + ":" + strings.Join(parts, sep)
}

func First[T any](ctx context.Context, xs []T) T { return xs[0] }

func Upper(s string) string { return strings.ToUpper(s) }

func main() {
	ctx := context.WithValue(context.Background(), ctxKey{}, "req")
	for _, n := range []ui.Node{Page{Tags: []string{"new"}}, (&Live{}).Render()} {
		out, err := ui.String(ctx, n)
		fmt.Println(out, err)
	}
}
