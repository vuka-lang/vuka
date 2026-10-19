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

func TestDropUnusedImports(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"package p\n\nimport \"github.com/vuka-lang/ui\"\n\nvar x = 1\n", "package p\n\n\nvar x = 1\n"},
		{"package p\n\nimport (\n\t\"fmt\"\n\t\"strings\"\n)\n\nvar x = fmt.Sprint(1)\n", "package p\n\nimport (\n\t\"fmt\"\n)\n\nvar x = fmt.Sprint(1)\n"},
		{"package p\n\nimport (\n\t\"fmt\"\n)\n\nvar x = 1\n", "package p\n\n\nvar x = 1\n"},
	} {
		if got := dropUnusedImports(c.src); got != c.want {
			t.Errorf("dropUnusedImports(%q) = %q, want %q", c.src, got, c.want)
		}
	}
}
