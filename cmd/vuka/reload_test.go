package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// A page with ui.JS.Reload in a push's Success, and a method returning it:
// the step renders in the hook, and the method's result is the command
// (vuka#445).
var reloadApp = map[string]string{
	"main.go": `package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"reflect"
	"regexp"
	"strings"

	"github.com/vuka-lang/ui/v2"
)

func main() {
	ctx := ui.WithPageURL(context.Background(), &url.URL{Path: "/orders", RawQuery: "page=2"}, nil)
	s, err := ui.String(ctx, &Orders{})
	if err != nil {
		panic(err)
	}
	s = html.UnescapeString(s)
	fmt.Println("hook", strings.Contains(s, ` + "`" + `"on_ok":[["reload",{}]]` + "`" + `))
	fmt.Println("on", strings.Contains(s, ` + "`" + `["add_class",{"to":{"sel":"#list"},"names":["dim"]}],["reload",{}]` + "`" + `))
	refs := regexp.MustCompile(` + "`" + `"m":"([^"]+)"` + "`" + `)
	for _, m := range refs.FindAllStringSubmatch(s, -1) {
		call, err := ui.ParsePushCall(ctx, m[1], "[]", "")
		if err != nil {
			panic(err)
		}
		out, err := call.Call(ctx, reflect.New(call.Receiver()))
		j, _ := json.Marshal(out)
		fmt.Println(call.Method(), string(j), err)
	}
}
`,
	"orders.vuka.html": `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

type Orders struct{}

func (p *Orders) Remove() error        { return nil }
func (p *Orders) Archive() ui.Commands { return ui.JS.Reload() }
</script>

<main>
  <button onClick={ui.JS.Push(Remove).Success(ui.JS.Reload())}>Remove</button>
  <button onClick={ui.JS.Push(Archive)}>Archive</button>
  <button onClick={ui.JS.On("#list").AddClass("dim").Reload()}>Refresh</button>
  <ul id="list" />
</main>
`,
}

func TestReloadStep(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a program")
	}
	gomod, gosum := testModule(t, "example.com/shop")
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	writeFile(t, filepath.Join(dir, "go.mod"), gomod)
	writeFile(t, filepath.Join(dir, "go.sum"), gosum)
	for name, src := range reloadApp {
		writeFile(t, filepath.Join(dir, name), src)
	}
	t.Setenv("GOFLAGS", "")
	t.Chdir(dir)
	var out, errOut bytes.Buffer
	if err := runGo("run", []string{"."}, &out, &errOut); err != nil {
		t.Fatalf("vuka run: %v\n%s%s", err, out.Bytes(), errOut.Bytes())
	}
	for _, want := range []string{
		"hook true", "on true",
		"Orders.Remove {} <nil>",
		`Orders.Archive {"js":[["reload",{}]]} <nil>`,
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("no %s in\n%s", want, out.String())
		}
	}
}
