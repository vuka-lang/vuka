// Package web is a stub of the folder-route API generated code calls.
package web

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

func PageRoute() RouteOption        { return option{} }
func LayoutOf[T any]() RouteOption  { return option{} }
func Receiver(ctor any) RouteOption { return option{} }
func GuardOf[T any]() RouteOption   { return option{} }
