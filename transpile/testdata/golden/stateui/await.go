package stateui

import (
	"context"
	"io"
	"time"
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
	Key                  string
	And                  []string
	Pending              Node
	Error                func(err error) Node
	Children             func() Node
	Wait, Delay, MinShow time.Duration
}

// Instant is Await's Delay for none.
const Instant time.Duration = -1

func (a Await) keys() []string { return append([]string{a.Key}, a.And...) }

func (a Await) state() (pending bool, err error) {
	for _, k := range a.keys() {
		if a.Of.Loading(k) {
			return true, nil
		}
		if err := a.Of.LoadErr(k); err != nil {
			return false, err
		}
	}
	return false, nil
}

func (a Await) view() Node {
	pending, err := a.state()
	switch {
	case pending:
		return a.Pending
	case err != nil:
		if a.Error == nil {
			return nil
		}
		return a.Error(err)
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
