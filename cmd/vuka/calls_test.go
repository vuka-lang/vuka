package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// A page that isn't live opening components of another package with
// ui.JS.Call: the compiler registers each component a Call names, and the
// method an Action prop is, so the token the page rendered opens the
// component, and its Action pushes the page's method over HTTP (vuka#440).
var callApp = map[string]string{
	"main.go": `package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"reflect"
	"regexp"

	"example.com/shop/components"
	"github.com/vuka-lang/ui/v2"
)

type host struct{}

func (host) FormToken(context.Context) string { return "tok" }
func (host) FormKey() []byte                  { return []byte("key") }
func (host) FormStore() ui.FormStore          { return nil }

func main() {
	ctx := ui.WithPageURL(ui.WithFormHost(context.Background(), host{}), &url.URL{Path: "/products"}, nil)
	s, err := ui.String(ctx, &Products{})
	if err != nil {
		panic(err)
	}
	s = html.UnescapeString(s)
	for _, m := range regexp.MustCompile(` + "`" + `"t":"([^"]+)"` + "`" + `).FindAllStringSubmatch(s, -1) {
		c, err := ui.ParseCall(ctx, "tok", m[1], json.RawMessage(` + "`" + `{"Query":"red"}` + "`" + `))
		if err != nil {
			c, err = ui.ParseCall(ctx, "tok", m[1], nil)
		}
		if err != nil {
			panic(err)
		}
		v, err := c.Value(nil)
		if err != nil {
			panic(err)
		}
		out, failed := ui.RenderCall(ctx, "k", v, c.Options()...)
		fmt.Println(c.Component(), failed, regexp.MustCompile(` + "`" + `<p>[^<]*</p>` + "`" + `).FindString(out))
		if e, ok := v.(*components.EditProduct); ok {
			ops, _ := ui.EncodeJS(ctx, e.OnSave.(ui.JSChain))
			ref := regexp.MustCompile(` + "`" + `"m":"([^"]+)"` + "`" + `).FindStringSubmatch(string(ops))[1]
			call, err := ui.ParsePushCall(ctx, ref, "[]", "")
			if err != nil {
				panic(err)
			}
			_, err = call.Call(ctx, reflect.New(call.Receiver()))
			fmt.Println(call.Method(), err)
		}
	}
}
`,
	"products.vuka.html": `<script lang="go">
package main

import (
	"example.com/shop/components"
	"github.com/vuka-lang/ui/v2"
)

type Products struct{}

func (p *Products) Reload()            {}
func (p *Products) Count() (int, error) { return 3, nil }
func (p *Products) Counted(n int)       {}
</script>

<main>
  <input id="q" value="red" />
  <button onClick={ui.JS.Call(components.EditProduct{ID: 7, Query: ui.Sel("#q").Value(), OnSave: Reload})}>Edit</button>
  <button onClick={ui.JS.Call(components.Details{ID: 8})}>Details</button>
  <button onClick={ui.JS.Call(components.Details{ID: 9}, ui.Into("#side"), ui.SizeLg, ui.CallKey("details"))}>Side</button>
  <button onClick={ui.JS.Push(Count).Success(Counted)}>Count</button>
  <div id="side" />
</main>
`,
	"components/editproduct.vuka.html": `<script lang="go">
package components

import (
	"strconv"

	"github.com/vuka-lang/ui/v2"
)

type EditProduct struct {
	ID     int64
	Query  any
	OnSave ui.Action
}
</script>

<ui.Dialog title={ui.Text("Edit")}>
  <p>{"product " + strconv.FormatInt(ID, 10) + " " + query()}</p>
  <button onClick={OnSave}>Save</button>
</ui.Dialog>
`,
	"components/details.vuka.html": `<script lang="go">
package components

import "strconv"

type Details struct{ ID int64 }
</script>

<section><p>{"details " + strconv.FormatInt(ID, 10)}</p></section>
`,
	"components/query.go": `package components

import "fmt"

func (e EditProduct) query() string { return fmt.Sprint(e.Query) }
`,
}

func TestCalls(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a program")
	}
	gomod, gosum := testModule(t, "example.com/shop")
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	writeFile(t, filepath.Join(dir, "go.mod"), gomod)
	writeFile(t, filepath.Join(dir, "go.sum"), gosum)
	for name, src := range callApp {
		writeFile(t, filepath.Join(dir, name), src)
	}
	t.Setenv("GOWORK", "off")
	t.Chdir(dir)
	var out, errOut bytes.Buffer
	if err := runGo("run", []string{"."}, &out, &errOut); err != nil {
		t.Fatalf("vuka run: %v\n%s%s", err, out.Bytes(), errOut.Bytes())
	}
	for _, want := range []string{
		"components.EditProduct <nil> <p>product 7 red</p>",
		"Products.Reload <nil>",
		"components.Details <nil> <p>details 8</p>",
		"components.Details <nil> <p>details 9</p>",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("no %s in\n%s", want, out.String())
		}
	}
}
