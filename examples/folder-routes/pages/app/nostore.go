// Package app is /app: a client-side app at [route...], with Settings, a
// server page, beside it.
package app

// NoStore marks a page main's web.WrapRoutes("no-store") never caches —
// an attribute a gate like auth's would be.
type NoStore struct{}

func (NoStore) RouteWrapper() string { return "no-store" }
