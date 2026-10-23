package web

import "context"

type Option interface{}

func ContextProcessors(processors ...any) Option { return nil }

func Browser(p any) any { return p }

func Personal(p any) any { return p }

func Auth(p any, gates ...any) any { return p }

func Var[T any](ctx context.Context, name string) T { var z T; return z }

type RequestInfo struct{ Path string }

type RequestVars struct{ Request RequestInfo }

func RequestContext(ctx context.Context) (RequestVars, error) { return RequestVars{}, nil }
