package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// A page that isn't live with a push's Before, Success, Error and After in
// its markup, methods among them: the compiler registers the methods a
// hook pushes as it does the push's own, so each is called over HTTP by
// the reference the page rendered (vuka#440).
var pushHooks = map[string]string{
	"main.go": `package main

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"reflect"
	"regexp"
	"strings"

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
	s = html.UnescapeString(s)
	for _, k := range []string{"before", "on_ok", "on_error", "after"} {
		fmt.Println(k, strings.Contains(s, "\""+k+"\":"))
	}
	refs := regexp.MustCompile(` + "`" + `"m":"([^"]+)"` + "`" + `)
	for _, m := range refs.FindAllStringSubmatch(s, -1) {
		call, err := ui.ParsePushCall(ctx, m[1], "[]", "")
		if err != nil {
			panic(err)
		}
		_, err = call.Call(ctx, reflect.New(call.Receiver()))
		fmt.Println(call.Method(), err)
	}
}
`,
	"orders.vuka.html": `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

type Orders struct{}

func (p *Orders) Remove() error  { return nil }
func (p *Orders) Refresh()       {}
func (p *Orders) Done() error    { return nil }
</script>

<main>
  <button onClick={ui.JS.Push(Remove).Before(ui.JS.Confirm("Remove the order?")).Success(Refresh).Error(ui.JS.SetText("#err", ui.LoadError)).After(Done)}>Remove</button>
  <p id="err" />
</main>
`,
}

func TestPushHooks(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a program")
	}
	gomod, gosum := testModule(t, "example.com/shop")
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	writeFile(t, filepath.Join(dir, "go.mod"), gomod)
	writeFile(t, filepath.Join(dir, "go.sum"), gosum)
	for name, src := range pushHooks {
		writeFile(t, filepath.Join(dir, name), src)
	}
	t.Setenv("GOFLAGS", "")
	t.Chdir(dir)
	var out, errOut bytes.Buffer
	if err := runGo("run", []string{"."}, &out, &errOut); err != nil {
		t.Fatalf("vuka run: %v\n%s%s", err, out.Bytes(), errOut.Bytes())
	}
	for _, want := range []string{
		"before true", "on_ok true", "on_error true", "after true",
		"Orders.Remove <nil>", "Orders.Refresh <nil>", "Orders.Done <nil>",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("no %s in\n%s", want, out.String())
		}
	}
}
