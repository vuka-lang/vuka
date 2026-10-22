// Package web stands in for github.com/vuka-lang/web from 2.0, which has
// no @web.Service: services are registered with app.Service.
package web

// App is an app.
type App struct{}

// Service registers a service's constructor.
func (a *App) Service(ctor any) *App { return a }

// Live declares a live page: a decorator on a type, gone in 2.0 (E0720).
func Live(path string) func(t any) { return nil }

// Get declares a route: on a function a handler, on a type a page (E0720).
func Get(path string) func(fn any) { return nil }
