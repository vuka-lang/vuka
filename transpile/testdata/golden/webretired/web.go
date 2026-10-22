// Package web stands in for github.com/vuka-lang/web from 2.0, which has
// no @web.Service: services are registered with app.Service.
package web

// App is an app.
type App struct{}

// Service registers a service's constructor.
func (a *App) Service(ctor any) *App { return a }
