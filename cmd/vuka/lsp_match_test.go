package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)


// matchSession is the proxy on a module holding src as main.vuka, opened.
type matchSession struct {
	t   *testing.T
	c   *lspClient
	uri string
	src string
}

func openMatch(t *testing.T, src string, init map[string]any) *matchSession {
	t.Helper()
	files := map[string]string{"main.vuka": src}
	var c *lspClient
	var dir string
	if init != nil {
		dir = t.TempDir()
		mod, sum := testModule(t, "lsptest")
		writeFile(t, filepath.Join(dir, "go.mod"), mod)
		writeFile(t, filepath.Join(dir, "go.sum"), sum)
		writeFile(t, filepath.Join(dir, "main.vuka"), src)
		t.Setenv("GOWORK", "off")
		c = startLSPInit(t, dir, lspOptions{renameCommands: true}, nil, init)
	} else {
		c, dir, _ = startLSPWith(t, files)
	}
	s := &matchSession{t: t, c: c, uri: pathToURI(filepath.Join(dir, "main.vuka")), src: src}
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": s.uri, "languageId": "vuka", "version": 1, "text": src}})
	return s
}

// at is the position of needle's byte delta in the session's text, after
// the nth (1-based) occurrence's start.
func (s *matchSession) at(needle string, delta int) lspPosition {
	s.t.Helper()
	i := strings.Index(s.src, needle)
	if i < 0 {
		s.t.Fatalf("no %q in the source", needle)
	}
	return positionOf([]byte(s.src), i+delta)
}

func (s *matchSession) span(needle string, delta, n int) lspRange {
	start := s.at(needle, delta)
	return lspRange{start, lspPosition{Line: start.Line, Character: start.Character + uint32(n)}}
}

func (s *matchSession) ask(method string, pos lspPosition, extra map[string]any) string {
	p := map[string]any{"textDocument": map[string]any{"uri": s.uri}, "position": pos}
	for k, v := range extra {
		p[k] = v
	}
	b, _ := json.Marshal(s.c.call(method, p))
	return string(b)
}

func (s *matchSession) change(version int, src string) {
	s.src = src
	s.c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": s.uri, "version": version},
		"contentChanges": []any{map[string]any{"text": src}}})
}

// rangeJSON is r as a decoded answer marshals it.
func rangeJSON(r lspRange) string {
	b, _ := json.Marshal(r)
	var v any
	_ = json.Unmarshal(b, &v)
	return jsonOf(v)
}

const matchSource = `package main

import (
	"errors"
	"strings"
)

var ErrNotFound = errors.New("not found")

type User struct {
	Email    string
	Password string
	Active   bool
}

func (u User) Name() string { return u.Email }

type Identity struct{ ID string }

enum Shape {
	Circle{Radius float64}
	Square{Side float64}
}

func findUser(login string) (User, error) { return User{Email: login}, nil }

func lookup(login string) Result[User] { return Ok(User{Email: login}) }

func maybe(login string) Option[User] { return None }

func identity(u User) (*Identity, error) { return &Identity{ID: u.Email}, nil }

func login(name string) (*Identity, string, error) {
	match findUser(strings.TrimSpace(name)) {
	case Ok(u):
		id := identity(u)?
		return id, u.Password, nil
	case Err(ErrNotFound):
		return nil, "", nil
	case Err(e):
		return nil, "", e
	}
}

func guarded(name string) string {
	match findUser(name) {
	case Ok(g) if !g.Active:
		return "inactive " + g.Email
	case Ok(User{Email: em}):
		return em
	case Err(_):
		return ""
	}
}

func result(name string) string {
	match lookup(name) {
	case Ok(ru):
		return ru.Email
	case Err(re):
		return re.Error()
	}
}

func option(name string) string {
	match maybe(name) {
	case Some(x):
		return x.Email
	case None:
		return ""
	}
}

func whole(name string) bool {
	match lookup(name) {
	case r:
		return r.IsOk()
	}
}

func area(s Shape) float64 {
	match s {
	case Shape.Circle{Radius: rad}:
		return 3 * rad * rad
	case Shape.Square{Side: side}:
		return side * side
	}
}

func main() {}
`

// TestLSPMatchBindings: a match's bindings are variables gopls knows, at
// their patterns: hover, definition, references, rename and semantic
// tokens there and at their uses, for every kind of pattern.
func TestLSPMatchBindings(t *testing.T) {
	s := openMatch(t, matchSource, nil)
	if ds := s.c.waitDiags(s.uri, func([]any) bool { return true }); len(ds) != 0 {
		t.Fatalf("diagnostics: %s", jsonOf(ds))
	}
	bindings := []struct {
		decl, use string // the binding's pattern and a use, each starting with the name's context
		dd, ud    int    // the name's offset in each
		name, typ string
	}{
		{"Ok(u)", "u.Password", 3, 0, "u", "User"},
		{"Err(e)", `"", e`, 4, 4, "e", "error"},
		{"Ok(g) if", "!g.Active", 3, 1, "g", "User"},
		{"Email: em", "return em", 7, 7, "em", "string"},
		{"Ok(ru)", "ru.Email", 3, 0, "ru", "User"},
		{"Err(re)", "re.Error", 4, 0, "re", "error"},
		{"Some(x)", "x.Email", 5, 0, "x", "User"},
		{"case r:", "r.IsOk", 5, 0, "r", "Result[User]"},
		{"Radius: rad", "3 * rad", 8, 4, "rad", "float64"},
	}
	for _, b := range bindings {
		want := "var " + b.name + " "
		for _, at := range []lspPosition{s.at(b.decl, b.dd), s.at(b.use, b.ud)} {
			if h := s.ask("textDocument/hover", at, nil); !strings.Contains(h, want) || !strings.Contains(h, b.typ) {
				t.Errorf("hover on %s at %v: %s", b.name, at, h)
			}
		}
		decl := rangeJSON(s.span(b.decl, b.dd, len(b.name)))
		if d := s.ask("textDocument/definition", s.at(b.use, b.ud), nil); !strings.Contains(d, decl) || !strings.Contains(d, "main.vuka") {
			t.Errorf("definition of %s: %s, want %s", b.name, d, decl)
		}
	}

	refs := s.ask("textDocument/references", s.at("Ok(u)", 3), map[string]any{"context": map[string]any{"includeDeclaration": true}})
	for _, r := range []lspRange{s.span("Ok(u)", 3, 1), s.span("identity(u)?", 9, 1), s.span("u.Password", 0, 1)} {
		if !strings.Contains(refs, rangeJSON(r)) {
			t.Errorf("references of u lack %s: %s", rangeJSON(r), refs)
		}
	}
	if strings.Contains(refs, "_vuka.go") {
		t.Errorf("references in the generated Go: %s", refs)
	}

	ren := s.ask("textDocument/rename", s.at("Ok(u)", 3), map[string]any{"newName": "user"})
	if strings.Count(ren, `"newText":"user"`) != 3 || !strings.Contains(ren, rangeJSON(s.span("Ok(u)", 3, 1))) || strings.Contains(ren, "_vuka.go") {
		t.Errorf("rename of u: %s", ren)
	}
	ren = s.ask("textDocument/rename", s.at("return em", 7), map[string]any{"newName": "email"})
	if strings.Count(ren, `"newText":"email"`) != 2 || !strings.Contains(ren, rangeJSON(s.span("Email: em", 7, 2))) {
		t.Errorf("rename of em: %s", ren)
	}

	legend := s.c.init.(map[string]any)["capabilities"].(map[string]any)["semanticTokensProvider"].(map[string]any)["legend"].(map[string]any)
	index := func(list any, name string) uint32 {
		for i, x := range list.([]any) {
			if x == name {
				return uint32(i)
			}
		}
		t.Fatalf("no %s in the legend %v", name, list)
		return 0
	}
	variable, declaration := index(legend["tokenTypes"], "variable"), index(legend["tokenModifiers"], "definition")
	toks := decodeTokens(t, s.c.call("textDocument/semanticTokens/full", map[string]any{"textDocument": map[string]any{"uri": s.uri}}).(map[string]any))
	for _, b := range bindings {
		at := s.at(b.decl, b.dd)
		found := false
		for _, tk := range toks {
			if tk.line == at.Line && tk.char == at.Character {
				found = true
				if tk.length != uint32(len(b.name)) || tk.typ != variable || tk.mods&(1<<declaration) == 0 {
					t.Errorf("token of %s at its pattern: %+v, want a variable's declaration", b.name, tk)
				}
			}
		}
		if !found {
			t.Errorf("no semantic token for %s at its pattern", b.name)
		}
	}
}

// TestLSPMatchCtorHover: hover on a pattern's Ok, Err, Some or None says
// what it matches.
func TestLSPMatchCtorHover(t *testing.T) {
	s := openMatch(t, matchSource, nil)
	s.c.waitDiags(s.uri, func([]any) bool { return true })
	for _, c := range []struct {
		needle string
		want   string
	}{
		{"Ok(u)", "`Ok` of `(User, error)`: matches when the error is nil, binding the `User`."},
		{"Err(ErrNotFound)", "`Err` of `(User, error)`: matches when the error isn't nil"},
		{"Ok(ru)", "`Ok` of `Result[User]`: matches a success, binding its `User`."},
		{"Err(re)", "`Err` of `Result[User]`: matches a failure"},
		{"Some(x)", "`Some` of `Option[User]`: matches a value, binding its `User`."},
		{"None:", "`None` of `Option[User]`: matches no value."},
	} {
		h := s.ask("textDocument/hover", s.at(c.needle, 1), nil)
		if !strings.Contains(h, c.want) || !strings.Contains(h, rangeJSON(s.span(c.needle, 0, strings.IndexAny(c.needle, "(:")))) {
			t.Errorf("hover on %s: %s", c.needle, h)
		}
	}
}

// TestLSPMatchMembers: completion after a binding's dot lists its type's
// members, and a misspelt one is an error on its name in the source.
func TestLSPMatchMembers(t *testing.T) {
	s := openMatch(t, matchSource, nil)
	s.c.waitDiags(s.uri, func([]any) bool { return true })
	labels := func(pos lspPosition) string {
		var out []string
		for l := range completionLabels(t, s.c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": s.uri}, "position": pos})) {
			out = append(out, l)
		}
		return strings.Join(out, ",")
	}
	got := labels(s.at("u.Password", 2))
	for _, want := range []string{"Email", "Password", "Active", "Name"} {
		if !strings.Contains(got, want) {
			t.Errorf("u. lacks %s: %s", want, got)
		}
	}
	src := strings.Replace(matchSource, "return nil, \"\", e\n", "println(e.Error())\n\t\treturn nil, \"\", e\n", 1)
	s.change(2, src)
	s.c.waitDiags(s.uri, func(ds []any) bool { return len(ds) == 0 })
	if got := labels(s.at("e.Error()", 2)); !strings.Contains(got, "Error") {
		t.Errorf("e. lacks Error: %s", got)
	}

	src = strings.Replace(matchSource, "u.Password, nil", "u.Pasword, nil", 1)
	s.change(3, src)
	want := rangeJSON(s.span("Pasword", 0, len("Pasword")))
	s.c.waitDiags(s.uri, func(ds []any) bool { return strings.Contains(jsonOf(ds), want) })
}

// TestLSPMatchInlayHints: gopls's type hints for := show a binding's type
// after its pattern name.
func TestLSPMatchInlayHints(t *testing.T) {
	s := openMatch(t, matchSource, map[string]any{"hints": map[string]any{"assignVariableTypes": true}})
	s.c.waitDiags(s.uri, func([]any) bool { return true })
	v := s.ask("textDocument/inlayHint", lspPosition{}, map[string]any{"range": lspRange{positionOf([]byte(matchSource), 0), positionOf([]byte(matchSource), len(matchSource))}})
	for _, b := range []struct {
		needle string
		delta  int
		label  string
	}{{"Ok(u)", 4, "User"}, {"Err(e)", 5, "error"}, {"Some(x)", 6, "User"}, {"Email: em", 9, "string"}} {
		pos := rangeJSON(lspRange{Start: s.at(b.needle, b.delta)})
		pos = pos[strings.Index(pos, `"start":`)+len(`"start":`) : len(pos)-1]
		if !strings.Contains(v, `"label":[{"value":"`+b.label+`"}],"paddingLeft":true,"position":`+pos) {
			t.Errorf("no hint %q after %s at %s: %s", b.label, b.needle, pos, v)
		}
	}
}

// TestLSPMatchPatternTypo: a misspelt Ok, Err, Some, None or variant is an
// error on its name with a did-you-mean and a quick fix writing it right.
func TestLSPMatchPatternTypo(t *testing.T) {
	for _, c := range []struct{ from, to, typo, fix, msg string }{
		{"case Err(ErrNotFound):", "case Error(ErrNotFound):", "Error", "Err", "Error(…) isn't a pattern"},
		{"case Ok(u):", "case ok(u):", "ok", "Ok", "ok(…) isn't a pattern"},
		{"case None:", "case Nnoe:", "Nnoe", "None", "Nnoe isn't a pattern"},
		{"case Shape.Circle{Radius: rad}:", "case Shape.Circel{Radius: rad}:", "Circel", "Circle", "Shape has no variant Circel"},
	} {
		t.Run(c.typo, func(t *testing.T) {
			src := strings.Replace(matchSource, c.from, c.to, 1)
			s := openMatch(t, src, nil)
			r := s.span(c.to, strings.Index(c.to, c.typo), len(c.typo))
			ds := s.c.waitDiags(s.uri, func(ds []any) bool { return strings.Contains(jsonOf(ds), "E0112") })
			var d map[string]any
			for _, x := range ds {
				if x.(map[string]any)["code"] == "E0112" {
					d = x.(map[string]any)
				}
			}
			msg, _ := d["message"].(string)
			if !strings.Contains(msg, c.msg) || !strings.Contains(msg, "did you mean "+c.fix+"?") || jsonOf(d["range"]) != rangeJSON(r) {
				t.Fatalf("diagnostic %s, want %q on %s", jsonOf(d), c.msg, rangeJSON(r))
			}
			acts := jsonOf(s.c.call("textDocument/codeAction", map[string]any{"textDocument": map[string]any{"uri": s.uri},
				"range": r, "context": map[string]any{"diagnostics": []any{d}}}))
			want := `{"newText":"` + c.fix + `","range":` + rangeJSON(r) + `}`
			if !strings.Contains(acts, `"title":"Change to `+c.fix+`"`) || !strings.Contains(acts, want) {
				t.Fatalf("code actions %s lack %s", acts, want)
			}
		})
	}
}

// TestLSPMatchCaseCompletion: completion after case offers the patterns
// that fit the subject, as snippets.
func TestLSPMatchCaseCompletion(t *testing.T) {
	s := openMatch(t, matchSource, nil)
	s.c.waitDiags(s.uri, func(ds []any) bool { return len(ds) == 0 })
	for i, c := range []struct {
		after string // the case the new one goes after
		want  []string
	}{
		{"\t\treturn nil, \"\", e\n", []string{`"label":"Ok(u)"`, `"newText":"Ok(${1:u}):"`, `"label":"Err(err)"`, `"label":"_"`, `pattern of (User, error)`}},
		{"\t\treturn re.Error()\n", []string{`"newText":"Ok(${1:user}):"`, `"label":"Err(err)"`, `pattern of Result[User]`}},
		{"\t\treturn x.Email\n", []string{`"label":"Some(user)"`, `"label":"None"`, `"newText":"None:"`}},
		{"\t\treturn side * side\n", []string{`"label":"Shape.Circle{…}"`, `"newText":"Shape.Circle{${1}}:"`, `"label":"Shape.Square{…}"`}},
	} {
		src := strings.Replace(matchSource, c.after, c.after+"\tcase Sh", 1)
		s.change(2+i, src)
		off := strings.Index(src, c.after) + len(c.after) + len("\tcase ")
		got := s.ask("textDocument/completion", positionOf([]byte(src), off), nil)
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("completion after %q lacks %s: %s", c.after, w, got)
			}
		}
	}
}

// TestLSPMatchExhaustive: a match missing a case is E0113 on its match
// and subject as the buffer changes, unsaved; its quick fix adds the
// case, returning the error; mended, the error goes.
func TestLSPMatchExhaustive(t *testing.T) {
	s := openMatch(t, matchSource, nil)
	s.c.waitDiags(s.uri, func(ds []any) bool { return len(ds) == 0 })
	broken := strings.Replace(matchSource, "\tcase Err(e):\n\t\treturn nil, \"\", e\n", "", 1)
	s.change(2, broken)
	r := rangeJSON(s.span("match findUser(strings", 0, len("match findUser(strings.TrimSpace(name))")))
	var d map[string]any
	start := time.Now()
	s.c.waitDiags(s.uri, func(ds []any) bool {
		for _, x := range ds {
			if m := x.(map[string]any); m["code"] == "E0113" {
				d = m
				return true
			}
		}
		return false
	})
	t.Logf("E0113 published %v after the change", time.Since(start).Round(time.Millisecond))
	msg := d["message"].(string)
	if !strings.HasPrefix(msg, "match on (User, error) isn't exhaustive: missing Err(_)\nhelp: add case Err(e): …, or case _:") || jsonOf(d["range"]) != r {
		t.Fatalf("E0113: %s, want it on %s", jsonOf(d), r)
	}
	if strings.Count(msg, "case _") != 1 {
		t.Errorf("the advice twice: %q", msg)
	}
	acts := s.c.call("textDocument/codeAction", map[string]any{"textDocument": map[string]any{"uri": s.uri},
		"range": d["range"], "context": map[string]any{"diagnostics": []any{d}}})
	var fixed string
	for _, a := range acts.([]any) {
		a := a.(map[string]any)
		if a["title"] != "Add missing cases" {
			continue
		}
		docs := a["edit"].(map[string]any)["documentChanges"].([]any)
		fixed = applyEdits(broken, docs[0].(map[string]any)["edits"].([]any))
	}
	if fixed != matchSource {
		t.Fatalf("the quick fix of %s gives:\n%s", jsonOf(acts), fixed)
	}
	s.change(3, fixed)
	s.c.waitDiags(s.uri, func(ds []any) bool { return !strings.Contains(jsonOf(ds), `"source":"vuka"`) })
}

// TestLSPMatchExhaustiveStays: E0113 stays through what else publishes
// the file's diagnostics, or builds again — a save, an edit to another
// file of the package, the same text again, gopls — until the match is
// mended.
func TestLSPMatchExhaustiveStays(t *testing.T) {
	other := "package main\n\nfunc helper() int { return 1 }\n"
	c, dir, _ := startLSPWith(t, map[string]string{"main.vuka": matchSource, "other.vuka": other})
	s := &matchSession{t: t, c: c, uri: pathToURI(filepath.Join(dir, "main.vuka")), src: matchSource}
	otherURI := pathToURI(filepath.Join(dir, "other.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": s.uri, "languageId": "vuka", "version": 1, "text": matchSource}})
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": otherURI, "languageId": "vuka", "version": 1, "text": other}})
	c.waitDiags(s.uri, func(ds []any) bool { return len(ds) == 0 })
	broken := strings.Replace(matchSource, "\tcase Err(e):\n\t\treturn nil, \"\", e\n", "", 1)
	s.change(2, broken)
	has := func(ds []any) bool { return strings.Contains(jsonOf(ds), `"code":"E0113"`) }
	c.waitDiags(s.uri, has)

	builds := func() int { return strings.Count(c.log.String(), "built the module") }
	settle := func(what string) {
		t.Helper()
		n := builds()
		for deadline := time.Now().Add(20 * time.Second); builds() == n && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		}
		time.Sleep(time.Second) // gopls's publications of the build's Go
		for {
			select {
			case p := <-c.diags:
				if ds, _ := p["diagnostics"].([]any); p["uri"] == s.uri && !has(ds) {
					t.Fatalf("after %s, E0113 went: %s\n%s", what, jsonOf(ds), c.log.String())
				}
				continue
			default:
			}
			break
		}
	}
	c.notify("textDocument/didSave", map[string]any{"textDocument": map[string]any{"uri": s.uri}})
	settle("a save")
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": otherURI, "version": 2},
		"contentChanges": []any{map[string]any{"text": other + "\nfunc helper2() int { return 2 }\n"}}})
	settle("an edit to another file")
	s.change(3, broken)
	settle("the same text again")
	c.notify("workspace/didChangeWatchedFiles", map[string]any{"changes": []any{map[string]any{"uri": otherURI, "type": 2}}})
	settle("a watched file's change")

	s.change(4, matchSource)
	c.waitDiags(s.uri, func(ds []any) bool { return !has(ds) })
}

// TestLSPDropInVukaDiagnostics: serving as gopls, the proxy reports on no
// .vuka file the editor hasn't opened in it: the editor's vuka server
// does, from the buffer, and a second set from disk would stand until a
// save.
func TestLSPDropInVukaDiagnostics(t *testing.T) {
	broken := strings.Replace(matchSource, "\tcase Err(e):\n\t\treturn nil, \"\", e\n", "", 1)
	c, dir, _ := startLSPOpts(t, map[string]string{"main.vuka": broken, "util.go": "package main\n\nfunc util() {}\n"}, lspOptions{dropIn: true})
	goURI := pathToURI(filepath.Join(dir, "util.go"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": goURI, "languageId": "go", "version": 1, "text": "package main\n\nfunc util() {}\n"}})
	c.waitDiags(goURI, func([]any) bool { return true })
	for _, p := range c.published {
		if strings.Contains(p, "main.vuka") {
			t.Fatalf("published for a .vuka file the editor didn't open: %s", p)
		}
	}
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); {
		select {
		case p := <-c.diags:
			if strings.HasSuffix(p["uri"].(string), "main.vuka") {
				t.Fatalf("published for a .vuka file the editor didn't open: %s", jsonOf(p))
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	vukaURI := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": vukaURI, "languageId": "vuka", "version": 1, "text": broken}})
	c.waitDiags(vukaURI, func(ds []any) bool { return strings.Contains(jsonOf(ds), "E0113") })
}
