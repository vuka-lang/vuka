// Package describe prints what a golden page's LiveSchema method declares.
package describe

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"vukatest.local/ui"
)

// Page is what x's LiveSchema declares; its Mount hook runs on x.
func Page(x any) string {
	l := ui.NewLiveSchema(x)
	x.(interface{ LiveSchema(*ui.LiveSchema) }).LiveSchema(l)
	var names []string
	for n := range l.Fields() {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "%s=%+v ", n, l.Fields()[n])
	}
	b.WriteString(strings.Join(l.Hooks(), " "))
	if m := l.MountHook(); m != nil {
		fmt.Fprintf(&b, " mount: %v", m(context.Background()))
	}
	return b.String()
}
