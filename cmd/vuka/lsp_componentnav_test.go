package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const navPasswordField = `<script lang="go">
package components

import "github.com/vuka-lang/ui/v2"

// PasswordField is a password input with a slot below it.
type PasswordField struct {
	Name     string
	Label    string
	Children ui.Node
}
</script>

<div>
  <label for={Name}>{Label}</label>
  <input id={Name} name={Name} type="password" />
  {Children}
</div>
`

const navApplicant = `<script lang="go">
package layouts

import "github.com/vuka-lang/ui/v2"

// Applicant is the applicant pages' layout.
type Applicant struct {
	Title    string
	Children ui.Node
}
</script>

<main>
  <h1>{Title}</h1>
  {Children}
</main>
`

const navRegister = `<script lang="go">
package pages

import (
	"example.com/app/components"
	"example.com/app/layouts"
)

// Register is a page using a layout and a component of other packages.
type Register struct{}

func (p *Register) Title() string { return heading("Register") }
</script>

<layouts.Applicant Title={Title()}>
  <form>
    <components.PasswordField Name="password" Label="Password">
      <small>Eight letters at least.</small>
    </components.PasswordField>
    <components.PasswordField name="confirm" label="Confirm" />
  </form>
</layouts.Applicant>
`

const navLogin = `<script lang="go">
package pages

import (
	"example.com/app/components"
	"example.com/app/layouts"
)

// Login uses the same layout and component.
type Login struct{}
</script>

<layouts.Applicant Title="Sign in">
  <components.PasswordField Name="password" Label="Password" />
</layouts.Applicant>
`

const navHelpers = `package pages

import "strings"

func heading(s string) string { return strings.TrimSpace(s) }
`

// TestLSPComponentNavigation: go to definition, type definition, references,
// hover and highlights on a component's tag, on a prop's attribute and in
// the component's own file answer in time, cold and warm, across packages,
// and land on the component's declaration, its prop fields and its uses in
// other packages' markup. A tag naming a package's component once deadlocked
// the proxy, which then answered every request with nothing at its deadline.
func TestLSPComponentNavigation(t *testing.T) {
	files := map[string]string{
		"components/password_field.vuka.html": navPasswordField,
		"layouts/applicant.vuka.html":         navApplicant,
		"pages/register.vuka.html":            navRegister,
		"pages/login.vuka.html":               navLogin,
		"pages/helpers.vuka":                  navHelpers,
	}
	app := routesModule(t, files)
	c := startLSPAt(t, app, lspOptions{renameCommands: true})
	uri := func(name string) string { return pathToURI(filepath.Join(app, name)) }
	for _, name := range []string{"pages/register.vuka.html", "components/password_field.vuka.html"} {
		c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
			"uri": uri(name), "languageId": "vuka-html", "version": 1, "text": files[name]}})
	}
	at := func(name, needle string, in int) map[string]any {
		t.Helper()
		i := strings.Index(files[name], needle)
		if i < 0 {
			t.Fatalf("%s has no %q", name, needle)
		}
		return map[string]any{"textDocument": map[string]any{"uri": uri(name)}, "position": positionOf([]byte(files[name]), i+in)}
	}
	// line is the line, from 0, of needle in the file name.
	line := func(name, needle string) uint32 {
		return positionOf([]byte(files[name]), strings.Index(files[name], needle)).Line
	}
	type loc struct {
		file string
		line uint32
	}
	locs := func(raw json.RawMessage) map[loc]bool {
		var list []map[string]any
		_ = json.Unmarshal(raw, &list)
		out := map[loc]bool{}
		for _, l := range list {
			u, _ := l["uri"].(string)
			r, ok := toRange(l["range"])
			if !ok {
				t.Errorf("a location without a range: %v", l)
			}
			rel, _ := filepath.Rel(app, uriToPath(u))
			out[loc{filepath.ToSlash(rel), r.Start.Line}] = true
		}
		return out
	}
	const comp, layout, reg, login = "components/password_field.vuka.html", "layouts/applicant.vuka.html", "pages/register.vuka.html", "pages/login.vuka.html"
	typeDecl := loc{comp, line(comp, "type PasswordField")}
	labelDecl := loc{comp, line(comp, "\tLabel ")}
	layoutDecl := loc{layout, line(layout, "type Applicant")}
	regUses := []loc{{reg, line(reg, "<components.PasswordField Name")}, {reg, line(reg, "<components.PasswordField name")}}
	loginUse := loc{login, line(login, "<components.PasswordField")}

	type check struct {
		what, method string
		params       map[string]any
		want         []loc // locations the answer holds
		has          string
	}
	refs := func(p map[string]any) map[string]any {
		p["context"] = map[string]any{"includeDeclaration": true}
		return p
	}
	checks := []check{
		{"definition on a component's tag", "textDocument/definition", at(reg, "<components.PasswordField Name", len("<components.Pass")), []loc{typeDecl}, ""},
		{"type definition on a component's tag", "textDocument/typeDefinition", at(reg, "<components.PasswordField Name", len("<components.Pass")), []loc{typeDecl}, ""},
		{"definition on a layout's tag", "textDocument/definition", at(reg, "<layouts.Applicant", len("<layouts.App")), []loc{layoutDecl}, ""},
		{"hover on a component's tag", "textDocument/hover", at(reg, "<components.PasswordField Name", len("<components.Pass")), nil, "PasswordField struct"},
		{"highlights of a component's tag", "textDocument/documentHighlight", at(reg, "<components.PasswordField Name", len("<components.Pass")), nil, "range"},
		{"references of a component's tag", "textDocument/references", refs(at(reg, "<components.PasswordField Name", len("<components.Pass"))), append([]loc{typeDecl, loginUse}, regUses...), ""},
		{"definition on a prop's attribute", "textDocument/definition", at(reg, `Label="Password"`, 2), []loc{labelDecl}, ""},
		{"hover on a prop's attribute", "textDocument/hover", at(reg, `Label="Password"`, 2), nil, "Label string"},
		{"references of a prop's attribute", "textDocument/references", refs(at(reg, `Label="Password"`, 2)), append([]loc{labelDecl, loginUse}, regUses...), ""},
		{"definition in the component's markup", "textDocument/definition", at(comp, "{Label}", 2), []loc{labelDecl}, ""},
		{"references of a prop in the component", "textDocument/references", refs(at(comp, "{Label}", 2)), append([]loc{labelDecl, loginUse}, regUses...), ""},
		{"references of the component's type", "textDocument/references", refs(at(comp, "type PasswordField", len("type Pass"))), append([]loc{typeDecl, loginUse}, regUses...), ""},
		{"highlights of the component's type", "textDocument/documentHighlight", at(comp, "type PasswordField", len("type Pass")), nil, "range"},
	}
	for _, round := range []string{"cold", "warm"} {
		for _, ch := range checks {
			start := time.Now()
			m := c.callWithin(answerDeadline, ch.method, ch.params)
			took := time.Since(start)
			if took >= goplsDeadline {
				t.Errorf("%s, %s: answered in %v, at the deadline: %s", round, ch.what, took, c.why())
			}
			if len(m.Error) > 0 {
				t.Errorf("%s, %s: %s", round, ch.what, m.Error)
				continue
			}
			if ch.has != "" && !strings.Contains(string(m.Result), ch.has) {
				t.Errorf("%s, %s: no %q in %s", round, ch.what, ch.has, m.Result)
			}
			if ch.method == "textDocument/documentHighlight" {
				var list []map[string]any
				_ = json.Unmarshal(m.Result, &list)
				for _, h := range list {
					if _, ok := toRange(h["range"]); !ok {
						t.Errorf("%s, %s: a highlight without a range: %s", round, ch.what, m.Result)
					}
				}
			}
			got := locs(m.Result)
			for _, w := range ch.want {
				if !got[w] {
					t.Errorf("%s, %s: no %s:%d in %s", round, ch.what, w.file, w.line+1, m.Result)
				}
			}
		}
	}

	// A cancelled request is answered at once.
	c.mu.Lock()
	c.next++
	id, _ := json.Marshal(c.next)
	reply := make(chan rpcMsg, 1)
	c.resps[string(id)] = reply
	c.mu.Unlock()
	_ = c.conn.send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": "textDocument/references",
		"params": refs(at(reg, "<components.PasswordField Name", len("<components.Pass")))})
	c.notify("$/cancelRequest", map[string]any{"id": json.RawMessage(id)})
	select {
	case <-reply:
	case <-time.After(2 * time.Second):
		t.Errorf("a cancelled references request wasn't answered in 2s")
	}
}

// TestJSXRequestUnlocked: the proxy asks jsxRequest with p.mu held, so on a
// tag naming another package's component it must not take p.mu (its import
// directory is read under it), or the proxy's loop stops for good.
func TestJSXRequestUnlocked(t *testing.T) {
	p, root := tagsModule(t)
	page := filepath.Join(root, "pages", "index.vuka.html")
	vf := &vfile{source: page, from: []byte(tagsPage)}
	off := strings.Index(tagsPage, "<components.Card") + len("<components.Ca")
	for _, method := range []string{"textDocument/hover", "textDocument/definition", "textDocument/declaration", "textDocument/references"} {
		done := make(chan struct{})
		p.mu.Lock()
		go func() {
			p.jsxRequest(method, map[string]any{}, vf, "", off)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s on <components.Card> waits for p.mu, held by its caller", method)
		}
		p.mu.Unlock()
	}
}
