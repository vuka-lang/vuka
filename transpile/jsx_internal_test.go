package transpile

import (
	"strings"
	"testing"
)

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

func TestGoParts(t *testing.T) {
	src := "import \"x\"\n\nfunc Row(o Order)\n<li class={cx(\"a\", o.C)}>A <code>@web.Live</code> {strings.Join(\n  o.Tags, \", \")}</li>\nif n := fmt.Sprint(o.N); n != \"\" {\n  <b>{n}</b>\n}\n"
	got := goParts([]byte(src))
	for _, want := range []string{"func Row(o Order)", `cx("a", o.C)`, "strings.Join(\n  o.Tags, \", \")", "if n := fmt.Sprint(o.N)"} {
		if !strings.Contains(got, want) {
			t.Errorf("goParts lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "web.") {
		t.Errorf("goParts has the markup's text:\n%s", got)
	}
}
