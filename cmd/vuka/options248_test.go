package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// A page loading a select's options from its own method, and a component
// on it from its: the compiler registers each method an Options command
// names (written bare in a template file's markup), so the page can call it
// by the reference it rendered (vuka#248).
var options248 = map[string]string{
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

func (host) FormToken(context.Context) string { return "" }
func (host) FormKey() []byte                  { return []byte("key") }
func (host) FormStore() ui.FormStore          { return nil }

func main() {
	ctx := ui.WithPageURL(ui.WithFormHost(context.Background(), host{}), &url.URL{Path: "/checkout"}, nil)
	s, err := ui.String(ctx, (&Checkout{}).Render())
	if err != nil {
		panic(err)
	}
	for _, m := range regexp.MustCompile(` + "`" + `"m":"([^"]+)","args":(\[[^\]]*\])` + "`" + `).FindAllStringSubmatch(html.UnescapeString(s), -1) {
		call, err := ui.ParseOptionsCall(ctx, m[1], ` + "`" + `["tz"]` + "`" + `)
		if err != nil {
			panic(err)
		}
		opts, err := call.Call(ctx, reflect.New(call.Receiver()))
		if err != nil {
			panic(err)
		}
		j, _ := json.Marshal(opts)
		fmt.Println(call.Method(), m[2], string(j))
	}
}
`,
	"checkout.vuka.html": `<script lang="go">
package main

import "github.com/vuka-lang/ui/v2"

type Checkout struct{}

func (p *Checkout) CitiesOf(country string) []ui.Option {
	return []ui.Option{{Value: country + "-dar", Label: "Dar es Salaam"}}
}
</script>

<main>
  <select id="country" name="country" onChange={ui.JS.Options(ui.Sel("#city"), CitiesOf, ui.This.Value())}></select>
  <select id="city" name="city"></select>
  <TownPicker/>
</main>
`,
	"town_picker.vuka.html": `<script lang="go">
package main

import (
	"context"

	"github.com/vuka-lang/ui/v2"
)

type TownPicker struct{}

func (t TownPicker) Towns(ctx context.Context, region string) ([]ui.OptionGroup, error) {
	return []ui.OptionGroup{{Label: region, Options: []ui.Option{{Value: "t1", Label: "Town"}}}}, nil
}
</script>

<select id="town" onChange={ui.JS.On(ui.This).Options(Towns, "north")}></select>
`,
}

func TestOptions248(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a program")
	}
	gomod, gosum := testModule(t, "example.com/shop")
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	writeFile(t, filepath.Join(dir, "go.mod"), gomod)
	writeFile(t, filepath.Join(dir, "go.sum"), gosum)
	for name, src := range options248 {
		writeFile(t, filepath.Join(dir, name), src)
	}
	t.Setenv("GOWORK", "off")
	t.Chdir(dir)
	var out, errOut bytes.Buffer
	if err := runGo("run", []string{"."}, &out, &errOut); err != nil {
		t.Fatalf("vuka run: %v\n%s%s", err, out.Bytes(), errOut.Bytes())
	}
	want := `Checkout.CitiesOf [{"$read":{"value":true}}] {"options":[{"value":"tz-dar","label":"Dar es Salaam"}]}
TownPicker.Towns ["north"] {"options":[{"group":"tz","options":[{"value":"t1","label":"Town"}]}]}
`
	if got := out.String(); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestOptions248Errors: an Options command whose arguments don't fit its
// method's parameters is a compile error (E0168).
func TestOptions248Errors(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a program")
	}
	for _, c := range []struct{ name, file, old, new, want string }{
		{"an argument too many", "checkout.vuka.html", `CitiesOf, ui.This.Value())`, `CitiesOf, ui.This.Value(), "x")`,
			"CitiesOf takes 1 argument (country string) and the step passes 2"},
		{"not a method", "checkout.vuka.html", `CitiesOf, ui.This.Value())`, `func(string) []ui.Option { return nil }, ui.This.Value())`,
			"ui.JS.Options takes a method of the page or of a component"},
		{"a value of another type", "town_picker.vuka.html", `Options(Towns, "north")`, `Options(Towns, 7)`,
			"7 is int, which Towns's 1st parameter, region string, can't take"},
	} {
		t.Run(c.name, func(t *testing.T) {
			gomod, gosum := testModule(t, "example.com/shop")
			dir, _ := filepath.EvalSymlinks(t.TempDir())
			writeFile(t, filepath.Join(dir, "go.mod"), gomod)
			writeFile(t, filepath.Join(dir, "go.sum"), gosum)
			for name, src := range options248 {
				if name == c.file {
					src = strings.Replace(src, c.old, c.new, 1)
				}
				writeFile(t, filepath.Join(dir, name), src)
			}
			t.Setenv("GOWORK", "off")
			t.Chdir(dir)
			var out, errOut bytes.Buffer
			err := runGo("build", []string{"-o", filepath.Join(dir, "app"), "."}, &out, &errOut)
			if got := out.String() + errOut.String(); err == nil || !strings.Contains(got, c.want) || !strings.Contains(got, "E0168") {
				t.Errorf("%v: no %q in\n%s", err, c.want, got)
			}
		})
	}
}
