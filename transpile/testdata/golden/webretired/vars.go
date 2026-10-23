package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"unicode"
)

type varsKey struct{}

// WithVars stands in for web.WithVars: s, a processor's result, gives its
// variables below ctx.
func WithVars(ctx context.Context, s any) context.Context {
	prev, _ := ctx.Value(varsKey{}).([]any)
	return context.WithValue(ctx, varsKey{}, append([]any{s}, prev...))
}

// Var stands in for web.Var: the variable name of the results WithVars
// gave.
func Var[T any](ctx context.Context, name string) T {
	all, _ := ctx.Value(varsKey{}).([]any)
	for _, s := range all {
		v := reflect.ValueOf(s)
		for i := 0; i < v.NumField(); i++ {
			if f := v.Type().Field(i); f.IsExported() && lowerCamel(f.Name) == name {
				return v.Field(i).Interface().(T)
			}
		}
	}
	panic(fmt.Sprintf("web.Var: no context processor gives %s", name))
}

func lowerCamel(name string) string {
	rs := []rune(name)
	n := 0
	for n < len(rs) && unicode.IsUpper(rs[n]) {
		n++
	}
	if n > 1 && n < len(rs) && unicode.IsLower(rs[n]) {
		n--
	}
	for i := 0; i < n; i++ {
		rs[i] = unicode.ToLower(rs[i])
	}
	return string(rs)
}

// RequestInfo is the current request, as web.RequestContext gives it.
type RequestInfo struct {
	Path   string
	Query  url.Values
	Params map[string]string
}

// RequestVars are web.RequestContext's variables.
type RequestVars struct{ Request RequestInfo }

// RequestContext stands in for the built-in processor.
func RequestContext(ctx context.Context) (RequestVars, error) { return RequestVars{}, nil }

// CSRFVars are web.CSRFContext's variables.
type CSRFVars struct{ CSRFToken string }

// CSRFContext stands in for the built-in processor.
func CSRFContext(ctx context.Context) (CSRFVars, error) { return CSRFVars{}, nil }

// Settings is a processor that isn't one: it returns no error.
func Settings(ctx context.Context) RequestVars { return RequestVars{} }

// New stands in for web.New.
func New(opts ...any) *App { return &App{} }

// ContextProcessors stands in for web.ContextProcessors.
func ContextProcessors(processors ...any) any { return processors }

// Request stands in for web.Request: the request being served.
func Request(ctx context.Context) *http.Request { return nil }
