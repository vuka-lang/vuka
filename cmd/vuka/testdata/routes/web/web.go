// Package web is a stub of the folder-route API generated code calls.
package web

import "net/http"

type Middleware = func(http.Handler) http.Handler

type RouteOption interface{ Attrs() []any }

type option struct{}

func (option) Attrs() []any { return nil }

type Registry struct{}

var Routes = &Registry{}

func (g *Registry) Get(path string, fn any, opts ...RouteOption) *Registry    { return g }
func (g *Registry) Post(path string, fn any, opts ...RouteOption) *Registry   { return g }
func (g *Registry) Put(path string, fn any, opts ...RouteOption) *Registry    { return g }
func (g *Registry) Patch(path string, fn any, opts ...RouteOption) *Registry  { return g }
func (g *Registry) Delete(path string, fn any, opts ...RouteOption) *Registry { return g }
func (g *Registry) Live(path string, page any, opts ...RouteOption) *Registry { return g }

type Rest string

type App struct{}

func (a *App) Get(path string, fn any, opts ...RouteOption) *App    { return a }
func (a *App) Post(path string, fn any, opts ...RouteOption) *App   { return a }
func (a *App) Put(path string, fn any, opts ...RouteOption) *App    { return a }
func (a *App) Patch(path string, fn any, opts ...RouteOption) *App  { return a }
func (a *App) Delete(path string, fn any, opts ...RouteOption) *App { return a }
func (a *App) Live(path string, page any, opts ...RouteOption) *App { return a }

type Option interface{ apply(*App) }

type OptionFunc func(*App)

func (f OptionFunc) apply(a *App) { f(a) }

func New(opts ...Option) *App {
	a := &App{}
	for _, o := range opts {
		o.apply(a)
	}
	return a
}

func Params(names ...string) RouteOption { return option{} }

func PageRoute() RouteOption        { return option{} }
func LayoutOf[T any]() RouteOption  { return option{} }
func Receiver(ctor any) RouteOption { return option{} }
func GuardOf[T any]() RouteOption   { return option{} }
func UseOf(mw any) RouteOption      { return option{} }
