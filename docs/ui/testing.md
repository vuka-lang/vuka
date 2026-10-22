# Testing

Test a shape's generated node from a `_test.go` logic file. Run `vuka test`
so shape compilation happens before the Go tests.

Save as `components/greeting.vuka.html`:

```html
package components

type Greeting struct { Name string }
<p>Hello, {Name}</p>
```

Then `components/greeting_test.go`:

```go
package components

import (
    "context"
    "testing"
    "github.com/vuka-lang/ui"
)

func TestGreeting(t *testing.T) {
    got, err := ui.String(context.Background(), Greeting{Name: "Ada"})
    if err != nil || got != "<p>Hello, Ada</p>" {
        t.Fatalf("%q %v", got, err)
    }
}
```

The test references the actual generated type, not a handwritten substitute.

## Stateful components

`ui/live/livetest` drives live sessions and applies protocol replies like a
browser. Test initial HTML, event handlers, validation failures and patches.
Stable keys should preserve child identity after insertion and reordering.

## JS commands

The reference client can Click and Fire DOM events, inspect visibility,
classes, attributes and text, and follow pushed handlers. Browser-state
assignments should update the DOM without a server event unless the attribute
also calls a server method. Use browser tests for effects the reference client
does not model, such as focus, scrolling and animations.

## Navigation

Test navigation and patches separately: check the URL, expected remounts,
retained state and permission checks on a changed resource. Session verification
is enabled in tests to catch output that changes without tracked dependencies.
