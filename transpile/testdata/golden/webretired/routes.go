package web

import (
	"context"
	"fmt"
)

// RouteQuery stands in for web.RouteQuery.
type RouteQuery map[string]any

// RouteURL stands in for web.RouteURL: the name and its arguments.
func RouteURL(ctx context.Context, name string, args ...any) string {
	return "/" + name + fmt.Sprint(args...)
}

// RoutePath stands in for web.RoutePath.
func RoutePath(ctx context.Context, name string, args ...any) string {
	return "/path/" + name + fmt.Sprint(args...)
}
