package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const unwrapStore = `package main

type User struct{ Name string }

type Store struct{}

func (Store) Users() ([]User, error)        { return nil, nil }
func (Store) Find(name string) (User, bool) { return User{name}, true }

func main() {}
`

const unwrapPage = `package main

// Page lists the store's users.
type Page struct{ Store Store }
<ul>
  for _, u := range Store.Users() {
    <li>{u.Name}</li>
  }
</ul>
`

// TestLSPUnwrapHover: hover on a (T, error) call in markup says what it is
// there, its T, under gopls's signature.
func TestLSPUnwrapHover(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.go": unwrapStore, "page.vuka.html": unwrapPage})
	uri := pathToURI(filepath.Join(dir, "page.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka-html", "version": 1, "text": unwrapPage}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
	var md string
	pos := positionOf([]byte(unwrapPage), strings.Index(unwrapPage, "Users()")+2)
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		h, _ := c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos}).(map[string]any)
		if h != nil {
			md = h["contents"].(map[string]any)["value"].(string)
			if strings.Contains(md, "func (Store) Users()") {
				break
			}
		}
	}
	for _, want := range []string{"func (Store) Users() ([]User, error)", "In markup this call is its `[]User`"} {
		if !strings.Contains(md, want) {
			t.Errorf("hover on Users lacks %q: %s", want, md)
		}
	}
}

// TestLSPUnwrapDiagnostic: a call markup can't unwrap is E0160 on the
// call's span.
func TestLSPUnwrapDiagnostic(t *testing.T) {
	page := strings.Replace(unwrapPage, "<li>{u.Name}</li>", `<li>{u.Name} {Store.Find("ann")}</li>`, 1)
	c, dir, _ := startLSPWith(t, map[string]string{"main.go": unwrapStore, "page.vuka.html": page})
	uri := pathToURI(filepath.Join(dir, "page.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka-html", "version": 1, "text": page}})
	var got string
	c.waitDiags(uri, func(ds []any) bool {
		got = jsonOf(ds)
		return strings.Contains(got, "E0160")
	})
	at := positionOf([]byte(page), strings.Index(page, `Store.Find("ann")`))
	end := positionOf([]byte(page), strings.Index(page, `Store.Find("ann")`)+len(`Store.Find("ann")`))
	want := fmt.Sprintf(`"range":{"end":{"character":%d,"line":%d},"start":{"character":%d,"line":%d}}`, end.Character, end.Line, at.Character, at.Line)
	if !strings.Contains(got, want) || !strings.Contains(got, "returns (User, bool)") {
		t.Errorf("diagnostics %s, want E0160 at %s", got, want)
	}
}
