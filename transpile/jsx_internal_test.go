package transpile

import "testing"

func TestJSXTextValue(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{" ", " "},
		{"\n\t\t", ""},
		{"\n\t\thello\n\t\tworld\n\t", "hello world"},
		{"  a  b  ", "  a  b  "},
		{"a\n\n  b", "a b"},
		{"x &amp; &lt;y&gt; &#39;&#x41;&nbsp;", "x & <y> 'A "},
	} {
		if got := jsxTextValue(c.in); got != c.want {
			t.Errorf("jsxTextValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPadNewlines(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"), ", 1, "), \n"},
		{`Text("a, b")`, 1, "Text(\n" + `"a, b")`},
		{`"x")`, 1, "\n" + `"x")`},
		{`f("x"), g(`, 2, `f("x"), g(` + "\n\n"},
		{`"a\"(" )`, 1, "\n" + `"a\"(" )`},
	} {
		if got := padNewlines(c.in, c.n); got != c.want {
			t.Errorf("padNewlines(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
