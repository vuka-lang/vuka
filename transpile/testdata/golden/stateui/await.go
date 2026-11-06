package stateui

import (
	"context"
	"io"
)

func (Live) vukaSchema(interface{ LiveSchema(*LiveSchema) }) {}

// LiveSchema collects a live component's deferred groups and their loaders.
type LiveSchema struct {
	groups map[string]any
	loads  map[string]func(context.Context) error
}

func (l *LiveSchema) Field(ptr any, attrs ...any) {
	for _, a := range attrs {
		if d, ok := a.(Defer); ok {
			if l.groups == nil {
				l.groups = map[string]any{}
			}
			l.groups[string(d)] = ptr
		}
	}
}

func (l *LiveSchema) Loads(group string, fn func(context.Context) error) {
	if l.loads == nil {
		l.loads = map[string]func(context.Context) error{}
	}
	l.loads[group] = fn
}

// LoadGroups runs the loaders c's LiveSchema declares, then each group is loaded
// or failed: what a component rendered without a session does.
func LoadGroups(c interface{ LiveSchema(*LiveSchema) }, l *Live) {
	var s LiveSchema
	c.LiveSchema(&s)
	l.errs = map[string]error{}
	for g, fn := range s.loads {
		l.errs[g] = fn(context.Background())
	}
}

// Loading is true until LoadGroups.
func (l *Live) Loading(group string) bool { return l.errs == nil }

func (l *Live) LoadErr(group string) error { return l.errs[group] }

// Await renders as ui.Await does.
type Await struct {
	Of interface {
		Loading(string) bool
		LoadErr(string) error
	}
	Key      string
	Pending  Node
	Error    func(err error) Node
	Children func() Node
}

func (a Await) view() Node {
	switch {
	case a.Of.Loading(a.Key):
		return a.Pending
	case a.Of.LoadErr(a.Key) != nil:
		if a.Error == nil {
			return nil
		}
		return a.Error(a.Of.LoadErr(a.Key))
	case a.Children == nil:
		return nil
	}
	return a.Children()
}

func (a Await) Render(ctx context.Context, w io.Writer) error {
	if n := a.view(); n != nil {
		return n.Render(ctx, w)
	}
	return nil
}
