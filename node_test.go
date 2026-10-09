package vuka_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/a-h/templ"
	"github.com/vuka-lang/vuka"
)

type stringer struct{}

func (stringer) String() string { return "<s>" }

var errBoom = errors.New("boom")

func failing() vuka.Node {
	return vuka.NodeFunc(func(context.Context, io.Writer) error { return errBoom })
}

func render(t *testing.T, n vuka.Node) string {
	t.Helper()
	s, err := vuka.String(context.Background(), n)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return s
}

func el(tag string, attrs ...vuka.Attr) vuka.Node { return vuka.El(tag, attrs) }

func TestRender(t *testing.T) {
	a := func(name string, v any) vuka.Attr { return vuka.Attr{Name: name, Value: v} }
	tests := []struct {
		name string
		n    vuka.Node
		want string
	}{
		{"text escaped", vuka.Text(`<a href="x">Tom & 'Jerry'</a>`), `&lt;a href=&#34;x&#34;&gt;Tom &amp; &#39;Jerry&#39;&lt;/a&gt;`},
		{"text number", vuka.Text(42), "42"},
		{"text nil", vuka.Text(nil), ""},
		{"element", vuka.El("div", nil, vuka.Text("hi")), "<div>hi</div>"},
		{"nested", vuka.El("ul", nil, vuka.El("li", nil, vuka.Text("a")), vuka.El("li", nil, vuka.Text("b"))), "<ul><li>a</li><li>b</li></ul>"},
		{"attr escaped", el("div", a("title", `"><script>x</script>`)), `<div title="&#34;&gt;&lt;script&gt;x&lt;/script&gt;"></div>`},
		{"attr quotes", el("div", a("title", `it's "q"`)), `<div title="it&#39;s &#34;q&#34;"></div>`},
		{"attr true", el("input", a("disabled", true)), `<input disabled>`},
		{"attr false", el("input", a("disabled", false)), `<input>`},
		{"attr nil", el("input", a("value", nil)), `<input>`},
		{"attr number", el("td", a("colspan", 2)), `<td colspan="2"></td>`},
		{"attr float", el("meter", a("value", 0.5)), `<meter value="0.5"></meter>`},
		{"attr stringer", el("div", a("title", stringer{})), `<div title="&lt;s&gt;"></div>`},
		{"attr node", el("div", a("title", vuka.El("b", nil, vuka.Text("x")))), `<div title="&lt;b&gt;x&lt;/b&gt;"></div>`},
		{"className", el("div", a("className", "card")), `<div class="card"></div>`},
		{"htmlFor", el("label", a("htmlFor", "name")), `<label for="name"></label>`},
		{"attr order kept", el("a", a("id", "x"), a("className", "y")), `<a id="x" class="y"></a>`},
		{"class list", el("div", a("className", []string{"a", "b"})), `<div class="a b"></div>`},
		{"class map", el("div", a("class", map[string]bool{"on": true, "off": false})), `<div class="on"></div>`},
		{"class KV", el("div", a("class", templ.KV("active", true))), `<div class="active"></div>`},
		{"style sorted", el("p", a("style", vuka.Style{"margin": "0", "color": "red"})), `<p style="color:red;margin:0;"></p>`},
		{"style unsafe value", el("p", a("style", vuka.Style{"background": "url(javascript:x)"})), `<p style="background:zTemplUnsafeCSSPropertyValue;"></p>`},
		{"style string", el("p", a("style", "color: red")), `<p style="color: red;"></p>`},
		{"href relative", el("a", a("href", "/users/1?x=a:b")), `<a href="/users/1?x=a:b"></a>`},
		{"href https", el("a", a("href", "HTTPS://x.dev")), `<a href="HTTPS://x.dev"></a>`},
		{"href mailto", el("a", a("href", "mailto:a@b.c")), `<a href="mailto:a@b.c"></a>`},
		{"href tel", el("a", a("href", "tel:+255")), `<a href="tel:+255"></a>`},
		{"href javascript", el("a", a("href", "javascript:alert(1)")), `<a href="about:invalid#TemplFailedSanitizationURL"></a>`},
		{"href JAVASCRIPT", el("a", a("href", "JAVASCRIPT:alert(1)")), `<a href="about:invalid#TemplFailedSanitizationURL"></a>`},
		{"href spaced javascript", el("a", a("href", " javascript:alert(1)")), `<a href="about:invalid#TemplFailedSanitizationURL"></a>`},
		{"href data", el("a", a("href", "data:text/html,x")), `<a href="about:invalid#TemplFailedSanitizationURL"></a>`},
		{"href SafeURL", el("a", a("href", templ.SafeURL("javascript:ok()"))), `<a href="javascript:ok()"></a>`},
		{"form action", el("form", a("action", "vbscript:x")), `<form action="about:invalid#TemplFailedSanitizationURL"></form>`},
		{"link href", el("link", a("href", "javascript:x")), `<link href="about:invalid#TemplFailedSanitizationURL">`},
		{"img src data image", el("img", a("src", "data:image/png;base64,AAAA")), `<img src="data:image/png;base64,AAAA">`},
		{"script attr", el("button", a("onclick", templ.JSUnsafeFuncCall("go()"))), `<button onclick="go()"></button>`},
		{"void", el("br"), "<br>"},
		{"doctype", vuka.El("html", nil, vuka.El("body", nil)), "<!DOCTYPE html><html><body></body></html>"},
		{"script text", vuka.El("script", nil, vuka.Text("</script><b>")), `<script>"\u003c/script\u003e\u003cb\u003e"</script>`},
		{"script value", vuka.El("script", nil, vuka.Safe("const u = "), vuka.Child("a\"b")), `<script>const u = "a\"b"</script>`},
		{"style text", vuka.El("style", nil, vuka.Text("a > b {} </style><b>")), `<style>a > b {} <\/style><b></style>`},
		{"text after script", vuka.Fragment(vuka.El("script", nil), vuka.Text("<")), `<script></script>&lt;`},
		{"safe", vuka.Safe("<b>raw</b>"), "<b>raw</b>"},
		{"child node", vuka.Child(vuka.Text("x")), "x"},
		{"child nil", vuka.Child(nil), ""},
		{"child string", vuka.Child("<x>"), "&lt;x&gt;"},
		{"child bool", vuka.Child(true), "true"},
		{"child int", vuka.Child(7), "7"},
		{"child float", vuka.Child(1.5), "1.5"},
		{"child stringer", vuka.Child(stringer{}), "&lt;s&gt;"},
		{"child error", vuka.Child(errors.New("<e>")), "&lt;e&gt;"},
		{"child nodes", vuka.Child([]vuka.Node{vuka.Text("a"), nil, vuka.Text("b")}), "ab"},
		{"child anys", vuka.Child([]any{"a", 1, nil, vuka.El("i", nil)}), "a1<i></i>"},
		{"child other", vuka.Child(struct{ A int }{1}), "{1}"},
		{"fragment", vuka.Fragment(vuka.Text("a"), nil, vuka.Text("b")), "ab"},
		{"nodes", vuka.Nodes(func(add func(vuka.Node)) {
			for i := range 3 {
				add(vuka.El("li", nil, vuka.Child(i)))
			}
		}), "<li>0</li><li>1</li><li>2</li>"},
		{"try ok", vuka.Try(vuka.Text("ok"), nil), "ok"},
		{"try nil", vuka.Try(nil, nil), ""},
		{"boundary ok", vuka.ErrorBoundary(func(error) vuka.Node { return vuka.Text("fb") }, vuka.Text("a"), vuka.Text("b")), "ab"},
		{"boundary fallback", vuka.El("div", nil, vuka.ErrorBoundary(
			func(err error) vuka.Node { return vuka.El("p", nil, vuka.Child(err)) },
			vuka.El("span", nil, vuka.Text("partial")), failing(),
		)), "<div><p>boom</p></div>"},
		{"boundary try", vuka.ErrorBoundary(func(err error) vuka.Node { return vuka.Child(err) }, vuka.Try(nil, errBoom)), "boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := render(t, tt.n); got != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestRenderErrors(t *testing.T) {
	tests := []struct {
		name string
		n    vuka.Node
		want string
	}{
		{"void children", vuka.El("img", nil, vuka.Text("x")), "void element"},
		{"attr name space", el("div", vuka.Attr{Name: "x onload", Value: "y"}), "invalid attribute name"},
		{"attr name quote", el("div", vuka.Attr{Name: `x"`, Value: "y"}), "invalid attribute name"},
		{"attr name empty", el("div", vuka.Attr{Name: "", Value: "y"}), "invalid attribute name"},
		{"tag name", vuka.El("di v", nil), "invalid tag name"},
		{"try", vuka.Try(vuka.Text("x"), errBoom), "boom"},
		{"child", vuka.El("div", nil, failing()), "boom"},
		{"nodes stop", vuka.Nodes(func(add func(vuka.Node)) { add(failing()); add(vuka.Text("x")) }), "boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := vuka.String(context.Background(), tt.n)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestNodesBuildsPerRender(t *testing.T) {
	calls := 0
	n := vuka.Nodes(func(add func(vuka.Node)) { calls++; add(vuka.Text(calls)) })
	if a, b := render(t, n), render(t, n); a != "1" || b != "2" {
		t.Fatalf("renders %q %q", a, b)
	}
}

type errWriter struct{ n int }

func (w *errWriter) Write(p []byte) (int, error) {
	w.n++
	return 0, errors.New("write failed")
}

func TestWriteError(t *testing.T) {
	w := &errWriter{}
	big := vuka.Text(strings.Repeat("x", 10000))
	err := vuka.El("div", nil, big, big, big).Render(context.Background(), w)
	if err == nil {
		t.Fatal("no error")
	}
	if w.n != 1 {
		t.Fatalf("%d writes after the first error", w.n-1)
	}
}

func TestTemplInterop(t *testing.T) {
	var c templ.Component = vuka.El("b", nil, vuka.Text("v")) // a Node is a templ component
	var n vuka.Node = templ.Raw("<i>t</i>")                   // and vice versa
	got := render(t, vuka.El("p", nil, c, n))
	if got != "<p><b>v</b><i>t</i></p>" {
		t.Fatal(got)
	}
}

func TestHandler(t *testing.T) {
	h := vuka.Handler(func(r *http.Request) (vuka.Node, error) {
		switch r.URL.Path {
		case "/err":
			return nil, errBoom
		case "/fail":
			return vuka.El("div", nil, vuka.Text("partial"), failing()), nil
		}
		return vuka.El("h1", nil, vuka.Text(r.URL.Query().Get("name"))), nil
	})
	tests := []struct {
		path, body, ctype string
		status            int
	}{
		{"/?name=<x>", "<h1>&lt;x&gt;</h1>", "text/html; charset=utf-8", 200},
		{"/err", "Internal Server Error\n", "text/plain; charset=utf-8", 500},
		{"/fail", "Internal Server Error\n", "text/plain; charset=utf-8", 500},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", tt.path, nil))
		if rec.Code != tt.status || rec.Body.String() != tt.body || rec.Header().Get("Content-Type") != tt.ctype {
			t.Errorf("%s: %d %q %q", tt.path, rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"))
		}
	}
}

func TestWrite(t *testing.T) {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	if err := vuka.Write(rec, r, vuka.El("div", nil, vuka.Text("x"), failing())); err == nil || rec.Body.Len() != 0 {
		t.Fatalf("err %v, body %q", err, rec.Body.String())
	}
	if err := vuka.Write(rec, r, vuka.El("p", nil)); err != nil || rec.Body.String() != "<p></p>" ||
		rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("err %v, body %q", err, rec.Body.String())
	}
}

func table(rows int) vuka.Node {
	return vuka.El("table", []vuka.Attr{{Name: "className", Value: "users"}},
		vuka.El("tbody", nil, vuka.Nodes(func(add func(vuka.Node)) {
			for i := range rows {
				add(vuka.El("tr", []vuka.Attr{{Name: "id", Value: i}, {Name: "className", Value: []string{"row", "even"}}},
					vuka.El("td", nil, vuka.Child(i)),
					vuka.El("td", nil, vuka.Text(fmt.Sprintf("user <%d>", i))),
					vuka.El("td", nil, vuka.El("a", []vuka.Attr{{Name: "href", Value: fmt.Sprintf("/users/%d", i)}}, vuka.Text("open"))),
				))
			}
		})))
}

func TestConcurrent(t *testing.T) {
	n := table(50)
	want := render(t, n)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				var b strings.Builder
				if err := n.Render(context.Background(), &b); err != nil || b.String() != want {
					t.Error("concurrent render differs")
					return
				}
			}
		}()
	}
	wg.Wait()
}

func BenchmarkTable1000(b *testing.B) {
	n := table(1000)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if err := n.Render(ctx, io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}
