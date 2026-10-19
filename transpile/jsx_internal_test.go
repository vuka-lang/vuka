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
