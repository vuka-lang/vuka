package term_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/a-h/templ"
	v "github.com/vuka-lang/vuka"
	"github.com/vuka-lang/vuka/term"
)

func a(name string, x any) v.Attr          { return v.Attr{Name: name, Value: x} }
func el(tag string, kids ...v.Node) v.Node { return v.El(tag, nil, kids...) }
func t(s string) v.Node                    { return v.Text(s) }

// lines joins its arguments as output lines.
func lines(ls ...string) string { return strings.Join(ls, "\n") + "\n" }

func TestString(t_ *testing.T) {
	tests := []struct {
		name  string
		n     v.Node
		width int
		want  string
	}{
		{"empty", v.Fragment(), 0, ""},
		{"text", t("  hello   world \n"), 0, lines("hello world")},
		{"h1", el("h1", t("Title")), 0, lines("Title", "═════")},
		{"h2", el("h2", t("Sub")), 0, lines("Sub", "───")},
		{"h3", el("h3", t("Small")), 0, lines("Small")},
		{"paragraphs", v.Fragment(el("p", t("one")), el("div", t("two")), el("section", el("p", t("three")))), 0,
			lines("one", "", "two", "", "three")},
		{"inline run between blocks", el("div", t("a "), el("b", t("b")), el("p", t("c")), t("d")), 0, lines("a b", "", "c", "", "d")},
		{"br", el("p", t("a"), el("br"), t("b"), el("br"), el("br"), t("c")), 0, lines("a", "b", "", "c")},
		{"hr", el("hr"), 0, lines(strings.Repeat("─", 40))},
		{"hr width", el("hr"), 10, lines(strings.Repeat("─", 10))},
		{"inline styles plain", el("p", el("b", t("b")), t(" "), el("i", t("i")), t(" "), el("u", t("u")), t(" "), el("code", t("x := 1"))), 0,
			lines("b i u `x := 1`")},
		{"pre", el("pre", t("\nif x {\n\treturn\n}\n")), 0, lines("  if x {", "      return", "  }")},
		{"blockquote", el("blockquote", el("p", t("a")), el("p", t("b"))), 0, lines("│ a", "│", "│ b")},
		{"ul", el("ul", el("li", t("a")), el("li", t("b"))), 0, lines("• a", "• b")},
		{"nested ul", el("ul", el("li", t("a"), el("ul", el("li", t("b"), el("ul", el("li", t("c"))))))), 0,
			lines("• a", "  ◦ b", "    ▪ c")},
		{"ol", v.El("ol", []v.Attr{a("start", 9)}, el("li", t("nine")), el("li", t("ten"))), 0, lines(" 9. nine", "10. ten")},
		{"li wraps under its text", el("ul", el("li", t("one two three four"))), 10, lines("• one two", "  three", "  four")},
		{"link", v.El("a", []v.Attr{a("href", "https://x.dev")}, t("docs")), 0, lines("docs (https://x.dev)")},
		{"link same text", v.El("a", []v.Attr{a("href", "https://x.dev")}, t("https://x.dev")), 0, lines("https://x.dev")},
		{"img", v.Fragment(v.El("img", []v.Attr{a("alt", "a cat")}), t(" "), v.El("img", nil)), 0, lines("[a cat] [image]")},
		{"table", el("table",
			el("thead", el("tr", el("th", t("Name")), el("th", t("Qty")))),
			el("tbody", el("tr", el("td", t("apple")), el("td", t("3"))), el("tr", el("td", t("kiwi")), el("td", t("12"))))), 0,
			lines("Name   Qty", "─────  ───", "apple  3", "kiwi   12")},
		{"table no header", el("table", el("tr", el("td", t("a")), el("td", t("bb"))), el("tr", el("td", t("ccc")), el("td", t("d")))), 0,
			lines("a    bb", "ccc  d")},
		{"button", el("p", t("go"), el("button", t("Save  now"))), 0, lines("go[ Save now ]")},
		{"inputs", el("form",
			v.El("input", []v.Attr{a("value", "ada")}), t(" "),
			v.El("input", []v.Attr{a("placeholder", "email")}), t(" "),
			v.El("input", []v.Attr{a("type", "checkbox")}), t(" "),
			v.El("input", []v.Attr{a("type", "radio"), a("checked", true)}), t(" "),
			v.El("input", []v.Attr{a("type", "hidden"), a("value", "x")}),
			v.El("input", []v.Attr{a("type", "submit")})), 0,
			lines("[ada_________] [email_______] [ ] (•) [ Submit ]")},
		{"atoms don't break", el("p", t("aa "), el("button", t("b b"))), 6, lines("aa", "[ b b ]")},
		{"unknown tags", el("x-card", el("span", t("in")), t("side")), 0, lines("inside")},
		{"skipped", el("div", el("script", t("var x")), el("style", t("p{}")), t("shown")), 0, lines("shown")},
		{"wrap", el("p", t("the quick brown fox jumps over the lazy dog")), 15, lines("the quick brown", "fox jumps over", "the lazy dog")},
		{"long word", el("p", t("a extraordinarily b")), 5, lines("a", "extraordinarily", "b")},
		{"safe html", v.Safe("<p>one&amp;<b>two</b></p><p>three</p>"), 0, lines("one&two three")},
		{"try", v.Try(el("p", t("ok")), nil), 0, lines("ok")},
		{"boundary", v.ErrorBoundary(func(err error) v.Node { return el("p", t("failed: "), v.Child(err)) },
			el("p", t("partial")), v.Try(nil, errors.New("boom"))), 0, lines("failed: boom")},
	}
	for _, tt := range tests {
		t_.Run(tt.name, func(t_ *testing.T) {
			got, err := term.String(context.Background(), tt.n, term.Options{Width: tt.width})
			if err != nil {
				t_.Fatal(err)
			}
			if got != tt.want {
				t_.Errorf("got:\n%s\nwant:\n%s\n%q", got, tt.want, got)
			}
		})
	}
}

func TestColor(t_ *testing.T) {
	n := v.Fragment(
		el("h1", t("Hi")),
		el("p", el("b", t("bold words")), t(" "), el("i", t("it")), t(" "), el("code", t("c")), t(" "),
			v.El("a", []v.Attr{a("href", "/x")}, t("link"))),
		el("table", el("tr", el("th", t("H"))), el("tr", el("td", t("d")))),
	)
	got, err := term.String(context.Background(), n, term.Options{Color: true})
	if err != nil {
		t_.Fatal(err)
	}
	want := lines(
		"\x1b[1mHi\x1b[0m", "══",
		"",
		"\x1b[1mbold words\x1b[0m \x1b[3mit\x1b[0m \x1b[7mc\x1b[0m \x1b[4mlink\x1b[0m (/x)",
		"",
		"\x1b[1mH\x1b[0m", "\x1b[2m─\x1b[0m", "d",
	)
	if got != want {
		t_.Errorf("got  %q\nwant %q", got, want)
	}
	plain, _ := term.String(context.Background(), n, term.Options{})
	if strings.Contains(plain, "\x1b") {
		t_.Errorf("escape codes without Color: %q", plain)
	}
}

// A page shaped like what the transpiler emits for JSX.
func TestPage(t_ *testing.T) {
	type user struct {
		Name  string
		Admin bool
	}
	users := []user{{"Ada", true}, {"Linus", false}}
	row := func(u user) v.Node {
		return v.El("tr", []v.Attr{a("className", "row")},
			v.El("td", nil, v.Child(u.Name)),
			v.El("td", nil, v.Nodes(func(add func(v.Node)) {
				if u.Admin {
					add(v.El("strong", nil, v.Text("admin")))
				} else {
					add(v.Text("user"))
				}
			})))
	}
	page := v.El("html", nil, v.El("head", nil, v.El("title", nil, v.Text("Users"))), v.El("body", nil,
		v.El("h2", nil, v.Text("Users ("), v.Child(len(users)), v.Text(")")),
		v.El("table", nil,
			v.El("tr", nil, v.El("th", nil, v.Text("Name")), v.El("th", nil, v.Text("Role"))),
			v.Nodes(func(add func(v.Node)) {
				for _, u := range users {
					add(row(u))
				}
			})),
		v.El("p", nil, v.Text("Showing all users.")),
	))
	got, err := term.String(context.Background(), page, term.Options{Width: 40})
	if err != nil {
		t_.Fatal(err)
	}
	want := lines("Users (2)", "─────────", "", "Name   Role", "─────  ─────", "Ada    admin", "Linus  user", "", "Showing all users.")
	if got != want {
		t_.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestOpaque(t_ *testing.T) {
	card := templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := io.WriteString(w, `<div class="card"><h3>Ada &amp; Co</h3><p>Hello<br>there</p><script>x()</script></div>`)
		return err
	})
	got, err := term.String(context.Background(), el("main", el("p", t("before")), card), term.Options{})
	if err != nil {
		t_.Fatal(err)
	}
	if want := lines("before", "", "Ada & Co Hello there"); got != want {
		t_.Errorf("got %q want %q", got, want)
	}
	fail := templ.ComponentFunc(func(context.Context, io.Writer) error { return errors.New("boom") })
	if _, err := term.String(context.Background(), el("p", fail), term.Options{}); err == nil {
		t_.Error("opaque error lost")
	}
}

func TestRender(t_ *testing.T) {
	var sb strings.Builder
	if err := term.Render(context.Background(), &sb, el("p", t("x")), term.Options{}); err != nil || sb.String() != "x\n" {
		t_.Fatalf("%q %v", sb.String(), err)
	}
}
