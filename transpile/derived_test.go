package transpile_test

import (
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// TestDerivedErrors: a Derived declared wrong is E0176, a write to one
// E0177, each at its place and naming what to do.
func TestDerivedErrors(t *testing.T) {
	head := "package main\n\nimport ui \"vukatest.local/stateui\"\n\ntype Cart struct {\n\tui.Live\n\tcount ui.State[int]\n\tprice ui.State[int]\n"
	for _, c := range []struct{ name, fields, rest, want string }{
		{"no From", "\tshown ui.Derived[int]\n", "", "E0176 cart.vuka.html:9:2: shown is a ui.Derived with no @ui.From"},
		{"From on a state", "\ttax ui.State[int] @ui.From(count * 2)\n", "", "E0176 cart.vuka.html:9:20: @ui.From goes on a ui.Derived field; tax is a stateui.State[int]"},
		{"no state read", "\tfixed ui.Derived[int] @ui.From(3)\n", "", "E0176 cart.vuka.html:9:33: @ui.From(3) reads no ui.State"},
		{"a prop", "\tover ui.Derived[bool] @ui.From(count > Limit)\n\tLimit int\n", "", "E0176 cart.vuka.html:9:33: @ui.From(count > Limit) reads Limit, which isn't browser state"},
		{"a cycle", "\ta ui.Derived[int] @ui.From(b + 1)\n\tb ui.Derived[int] @ui.From(c + count)\n\tc ui.Derived[int] @ui.From(a * 2)\n", "", "E0176 cart.vuka.html:9:20: Derived fields read each other: a → b → c → a"},
		{"an operator", "\tavg ui.Derived[int] @ui.From(count / price)\n", "", "E0124 cart.vuka.html:9:31: the / operator can't run in the browser"},
		{"a misspelt name", "\ttotal ui.Derived[int] @ui.From(cout * price)\n", "", "E0176 cart.vuka.html:9:33: @ui.From reads cout, which Cart has no field named and the package doesn't declare; did you mean count?"},
		{"written in markup", "\ttotal ui.Derived[int] @ui.From(count * price)\n", "<button onClick={total++}>+</button>\n", "E0177 cart.vuka.html:11:18: total is a ui.Derived: the browser computes it"},
		{"written in a method", "\ttotal ui.Derived[int] @ui.From(count * price)\n", "func (c *Cart) Clear() {\n\tc.total = 0\n}\n<p>{total}</p>\n", "E0177 cart.vuka.html:12:2: c.total is a ui.Derived"},
		{"Notify on a Derived", "\ttotal ui.Derived[int] @ui.From(count * price) @ui.Notify(Seen)\n", "func (c *Cart) Seen(n int) {}\n<p>{total}</p>\n", "E0178 cart.vuka.html:9:48: @ui.Notify goes on a ui.State field; total is a stateui.Derived[int]"},
		{"Notify naming no method", "\ttab ui.State[string] @ui.Notify(OnTba)\n", "func (c *Cart) OnTab(tab string) {}\n<p>{tab}</p>\n", "E0178 cart.vuka.html:9:34: @ui.Notify names OnTba, which isn't a method of Cart; did you mean OnTab?"},
		{"Notify's handler of another type", "\ttab ui.State[string] @ui.Notify(OnTab)\n", "func (c *Cart) OnTab(n int) {}\n<p>{tab}</p>\n", "E0178 cart.vuka.html:9:34: OnTab takes (n int); @ui.Notify on tab calls it with one string; declare it func (…) OnTab(tab string)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			rest := c.rest
			if !strings.Contains(rest, "<") {
				rest += "<p>{count}</p>\n"
			}
			src := head + c.fields + "}\n" + rest
			_, err := transpile.Package([]transpile.File{{Name: "cart.vuka.html", Src: []byte(src)}, {Name: "main.go", Src: []byte("package main\n\nfunc main() {}\n")}},
				transpile.Options{Bare: true, Importer: load.NewImporter("testdata/golden", "")})
			if err == nil {
				t.Fatalf("no error; want %s", c.want)
			}
			var got []string
			list, _ := err.(transpile.ErrorList)
			for _, e := range list {
				got = append(got, string(e.Code)+" "+e.Error())
			}
			if !strings.Contains(strings.Join(got, "\n"), c.want) {
				t.Errorf("errors:\n%s\nwant %s", strings.Join(got, "\n"), c.want)
			}
		})
	}
}
