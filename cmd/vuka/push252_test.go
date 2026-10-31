package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// A page that isn't live pushing to its own methods and a component's,
// each result kind, written bare in a template file's markup: the compiler
// registers each, so the page can call it by the reference it rendered, its
// arguments positional or a struct from the form (vuka#252). The live page
// beside it registers only what it reads over HTTP.
var push252 = map[string]string{
	"main.go": `package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"reflect"
	"regexp"

	"github.com/vuka-lang/ui/v2"
)

type host struct{}

func (host) FormToken(context.Context) string { return "tok" }
func (host) FormKey() []byte                  { return []byte("key") }
func (host) FormStore() ui.FormStore          { return nil }

func main() {
	ctx := ui.WithPageURL(ui.WithFormHost(context.Background(), host{}), &url.URL{Path: "/orders"}, nil)
	s, err := ui.String(ctx, &Orders{})
	if err != nil {
		panic(err)
	}
	calls := []struct{ args, form string }{
		{"[7]", ""},
		{` + "`" + `["tea","3","SAVE"]` + "`" + `, ""},
		{"", ` + "`" + `{"sku":["tea"],"qty":["4"],"code":["X"]}` + "`" + `},
		{` + "`" + `["0100"]` + "`" + `, ""},
		{` + "`" + `["ann"]` + "`" + `, ""},
		{"[1]", ""},
		{` + "`" + `["tz"]` + "`" + `, ""},
	}
	refs := regexp.MustCompile(` + "`" + `"m":"([^"]+)"` + "`" + `)
	for i, m := range refs.FindAllStringSubmatch(html.UnescapeString(s), -1) {
		call, err := ui.ParsePushCall(ctx, m[1], calls[i].args, calls[i].form)
		if err != nil {
			panic(err)
		}
		out, err := call.Call(ctx, reflect.New(call.Receiver()))
		j, _ := json.Marshal(out)
		fmt.Println(call.Method(), call.Read(), string(j), err)
	}
	feed, err := ui.String(ctx, (&Feed{}).Render())
	if err != nil {
		panic(err)
	}
	fmt.Println("the live page's references:", len(refs.FindAllString(html.UnescapeString(feed), -1)))
}
`,
	"types.go": `package main

type Quote struct {
	SKU  string
	Qty  int
	Code string
}

type Place struct{ City, Region string }
`,
	"orders.vuka.html": `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

type Orders struct{}

func (p *Orders) Details(id int64) (ui.Node, error) { return ui.Text("order"), nil }
func (p *Orders) Price(sku string, qty int, coupon string) string { return sku + coupon }
func (p *Orders) Total(q Quote) (int, error) { return q.Qty * 10, nil }
func (p *Orders) Lookup(postcode string) (Place, error) { return Place{"Arusha", postcode}, nil }
func (p *Orders) Check(name string) ui.Commands { return ui.JS.SetText("#msg", name) }
func (p *Orders) Remove(id int64) error { return nil }
</script>

<main>
  <button onClick={ui.JS.Push(Details, ui.Value(int64(7)), ui.Read()).Into("#details")}>View</button>
  <form>
    <input name="qty" onInput={ui.JS.Push(Price, "tea", ui.This.Value(), ui.Sel("[name=code]").Value(), ui.Read()).Into("#price")} />
    <input name="code" onInput={ui.JS.Push(Total, ui.Read()).Into("#total")} />
    <input name="postcode" onChange={ui.JS.Push(Lookup, ui.This.Value()).Into(ui.Closest("form"))} />
  </form>
  <input id="name" onInput={ui.JS.Push(Check, ui.This.Value(), ui.Read())} />
  <button onClick={ui.JS.Push(Remove, int64(1))}>Remove</button>
  <Regions/>
  <div id="details" /><span id="price" /><span id="total" /><p id="msg" />
</main>
`,
	"regions.vuka.html": `<script lang="go">
package main

import (
	"context"

	"github.com/vuka-lang/ui/v2"
)

type Regions struct{}

func (r Regions) Of(ctx context.Context, country string) ([]ui.Option, error) {
	return []ui.Option{{Value: country, Label: "Region"}}, nil
}
</script>

<select id="region" onChange={ui.JS.Push(Of, ui.This.Value()).Into(ui.This)}></select>
`,
	"feed.vuka.html": `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

type Feed struct {
	ui.Live
	n int
}

func (p *Feed) Bump(by int) (string, error) { p.n += by; return "", nil }
func (p *Feed) Older(before int) ui.Node { return ui.Text("older") }
</script>

<main>
  <button onClick={ui.JS.Push(Bump, 1).Into("#n")}>{n}</button>
  <button onClick={ui.JS.Push(Older, 5, ui.Read()).Into("#posts", ui.Append())}>Older</button>
  <span id="n" /><ul id="posts" />
</main>
`,
}

func writePush252(t *testing.T, file, old, new string) string {
	t.Helper()
	gomod, gosum := testModule(t, "example.com/shop")
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	writeFile(t, filepath.Join(dir, "go.mod"), gomod)
	writeFile(t, filepath.Join(dir, "go.sum"), gosum)
	for name, src := range push252 {
		if name == file {
			if !strings.Contains(src, old) {
				t.Fatalf("no %q in %s", old, name)
			}
			src = strings.Replace(src, old, new, 1)
		}
		writeFile(t, filepath.Join(dir, name), src)
	}
	t.Setenv("GOWORK", "off")
	t.Chdir(dir)
	return dir
}

func TestPush252(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a program")
	}
	writePush252(t, "", "", "")
	var out, errOut bytes.Buffer
	if err := runGo("run", []string{"."}, &out, &errOut); err != nil {
		t.Fatalf("vuka run: %v\n%s%s", err, out.Bytes(), errOut.Bytes())
	}
	for _, want := range []string{
		`Orders.Details true {"html":"order"} <nil>`,
		`Orders.Price true {"value":"teaSAVE"} <nil>`,
		`Orders.Total true {"value":"40"} <nil>`,
		`Orders.Lookup false {"fields":{"city":"Arusha","region":"0100"}} <nil>`,
		`Orders.Check true {"js":[["set_text",{"to":{"sel":"#msg"},"text":"ann"}]]} <nil>`,
		`Orders.Remove false {} <nil>`,
		`Regions.Of false {"options":[{"value":"tz","label":"Region"}]} <nil>`,
		"the live page's references: 1",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("no %s in\n%s", want, out.String())
		}
	}
}

// TestPush252Errors: a step that can't call its handler is a compile error
// (E0168).
func TestPush252Errors(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a program")
	}
	for _, c := range []struct{ name, old, new, want string }{
		{"too few", `Push(Price, "tea", ui.This.Value(), `, `Push(Price, "tea", `,
			"Price takes 3 arguments (sku string, qty int, coupon string) and the step passes 2"},
		{"a value of another type", `Push(Remove, int64(1))`, `Push(Remove, "one")`,
			`"one" is string, which Remove's 1st parameter, id int64, can't take`},
		{"Into without a result", `Push(Remove, int64(1))`, `Push(Remove, int64(1)).Into("#msg")`,
			"Remove returns no result to put Into a target"},
		{"a result it can't apply", `func (p *Orders) Remove(id int64) error { return nil }`, `func (p *Orders) Remove(id int64) map[string]int { return nil }`,
			"Remove returns map[string]int"},
		{"Append among the arguments", `Push(Remove, int64(1))`, `Push(Remove, int64(1), ui.Append())`,
			"goes in Into"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := writePush252(t, "orders.vuka.html", c.old, c.new)
			var out, errOut bytes.Buffer
			err := runGo("build", []string{"-o", filepath.Join(dir, "app"), "."}, &out, &errOut)
			if got := out.String() + errOut.String(); err == nil || !strings.Contains(got, c.want) || !strings.Contains(got, "E0168") {
				t.Errorf("%v: no %q in\n%s", err, c.want, got)
			}
		})
	}
}

// TestPush252Warnings: a stateless push's write is lost (W0113); a live
// page's isn't, and the build goes on.
func TestPush252Warnings(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a program")
	}
	dir := writePush252(t, "orders.vuka.html", `type Orders struct{}`, "type Orders struct{ seen int64 }")
	src := push252["orders.vuka.html"]
	src = strings.Replace(src, `type Orders struct{}`, "type Orders struct{ seen int64 }", 1)
	src = strings.Replace(src, `func (p *Orders) Remove(id int64) error { return nil }`, `func (p *Orders) Remove(id int64) error { p.seen = id; return nil }`, 1)
	writeFile(t, filepath.Join(dir, "orders.vuka.html"), src)
	var out, errOut bytes.Buffer
	if err := runGo("build", []string{"-o", filepath.Join(dir, "app"), "."}, &out, &errOut); err != nil {
		t.Fatalf("a warning fails the build: %v\n%s%s", err, out.Bytes(), errOut.Bytes())
	}
	got := out.String() + errOut.String()
	if !strings.Contains(got, "W0113") || !strings.Contains(got, "Remove assigns p.seen") {
		t.Errorf("no W0113 in\n%s", got)
	}
	if strings.Contains(got, "Bump assigns") {
		t.Errorf("a warning for the live page's push:\n%s", got)
	}
}
