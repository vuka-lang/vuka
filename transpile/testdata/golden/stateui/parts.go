package stateui

import (
	"context"
	"io"

	ui "github.com/vuka-lang/ui/v2"
)

// Ctx is the node build makes of the context it renders with.
func Ctx(build func(ctx context.Context) Node) Node { return ui.Ctx(build) }

// Defer marks a method whose part renders after the page's first bytes: in
// these cases, as Part renders it, in place.
type Defer string

// PartFallback is a part's fallbacks while its method runs and when it fails.
type PartFallback struct{ Pending, Error Node }

func (f PartFallback) Render(ctx context.Context, w io.Writer) error { return nil }

// Part renders render's markup of m's result, or the fallback's Error when
// m fails: in place, as a page rendered where nothing streams does.
func Part[R, V any](recv R, m func(R, context.Context) (V, error), fallback Node, render func(V) Node) Node {
	return ui.Ctx(func(ctx context.Context) Node {
		v, err := m(recv, ctx)
		if err != nil {
			if f, ok := fallback.(PartFallback); ok && f.Error != nil {
				return f.Error
			}
			return HTML(`<span role="alert">failed</span>`)
		}
		return render(v)
	})
}

// Suspense renders body in place, error when it fails: a page rendered
// where nothing streams.
func Suspense(fallback, error Node, body func(ctx context.Context) Node) Node {
	return ui.Ctx(func(ctx context.Context) (n Node) {
		defer func() {
			if r := recover(); r != nil {
				n = error
				if n == nil {
					n = HTML(`<span role="alert">failed</span>`)
				}
			}
		}()
		s, err := ui.String(ctx, body(ctx))
		if err != nil {
			panic(err)
		}
		return ui.RawHTML(s)
	})
}

func LazyValue[T any](ctx context.Context, run func(context.Context) (T, error)) T {
	v, err := run(ctx)
	if err != nil {
		panic(err)
	}
	return v
}
func LazyPart(fallback, failed Node, body func(context.Context) Node, group ...string) Node {
	if f, ok := fallback.(PartFallback); ok {
		fallback, failed = f.Pending, f.Error
	}
	return Suspense(fallback, failed, body)
}
