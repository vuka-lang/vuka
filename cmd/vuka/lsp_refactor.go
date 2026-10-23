package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// Refactors and quick fixes. vuka lsp adds code actions of its own to
// gopls's on the generated Go: on a .vuka.html shape file, extracting a
// selection of its markup into a component of its own and moving its methods
// to the logic file beside it; and on the diagnostics that have one obvious
// fix, that fix. They are worked out from the files' text, and where a
// type matters, from a type-check of the package's generated Go
// (lsp_refactor_types.go).
//
// The actions' edits are WorkspaceEdits with document changes (a file
// created, edited or deleted). Extracting a component needs a name: its
// action runs the command vuka.extractComponent, which the VS Code
// extension asks a name for before it reaches the server; another editor
// sends it as it is, and the server picks one. The server applies the edit
// with workspace/applyEdit.

// cmdExtract is the command extracting a component: arguments
// [{uri, range, name?}].
const cmdExtract = "vuka.extractComponent"

// refactorKinds are the kinds of the code actions vuka adds, advertised
// with gopls's.
var refactorKinds = []string{"quickfix", "refactor.extract", "refactor.move", "refactor.rewrite"}

var (
	// pendingActions holds the code actions vuka worked out for a request on a
	// .vuka file, by the editor's request id, until gopls's answer joins them.
	pendingActions sync.Map
	// editorResolves records whether the editor resolves a code action's edit
	// lazily (codeAction/resolve), as its initialize said.
	editorResolves atomic.Bool
	// applies are the proxy's workspace/applyEdit requests awaiting the
	// editor's answer.
	applies   sync.Map
	applySeq  atomic.Int64
	applyWait = 30 * time.Second
)

// refactorRequest takes the editor's requests that concern vuka's own code
// actions: it answers executeCommand and codeAction/resolve for them, and
// for a code action request works out vuka's actions, joining them to
// gopls's. It reports whether it answered the request.
func (p *proxy) refactorRequest(m *rpcMsg, path string) bool {
	switch m.Method {
	case "initialize":
		var params struct {
			Capabilities struct {
				TextDocument struct {
					CodeAction struct {
						ResolveSupport *struct {
							Properties []string `json:"properties"`
						} `json:"resolveSupport"`
					} `json:"codeAction"`
				} `json:"textDocument"`
			} `json:"capabilities"`
		}
		_ = json.Unmarshal(m.Params, &params)
		rs := params.Capabilities.TextDocument.CodeAction.ResolveSupport
		editorResolves.Store(rs != nil && slicesContains(rs.Properties, "edit"))
		return false
	case "workspace/executeCommand":
		var params struct {
			Command   string            `json:"command"`
			Arguments []json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(m.Params, &params) != nil || params.Command != cmdExtract {
			return false
		}
		go p.runExtract(m.ID, params.Arguments)
		return true
	case "codeAction/resolve":
		var action map[string]any
		if json.Unmarshal(m.Params, &action) != nil {
			return false
		}
		data, _ := action["data"].(map[string]any)
		if data == nil || data["vuka"] == nil {
			return false
		}
		go func() {
			if e := p.resolveAction(data); e != nil {
				action["edit"] = e
			}
			_ = p.editor.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": action})
		}()
		return true
	case "textDocument/codeAction":
	default:
		return false
	}
	if path == "" || !isVuka(path) && !strings.HasSuffix(path, ".go") {
		return false
	}
	var params struct {
		Range   lspRange `json:"range"`
		Context struct {
			Diagnostics []map[string]any `json:"diagnostics"`
			Only        []string         `json:"only"`
		} `json:"context"`
	}
	if json.Unmarshal(m.Params, &params) != nil {
		return false
	}
	real := p.real(path)
	src, ok := p.buffer(real)
	if !ok {
		return false
	}
	req := actionRequest{path: real, src: src, start: offsetOf(src, params.Range.Start), end: offsetOf(src, params.Range.End),
		diags: params.Context.Diagnostics, only: params.Context.Only, read: p.readFunc(), uri: p.display}
	actions := codeActions(req)
	if len(actions) == 0 {
		return false
	}
	if isVuka(path) || p.routeGo(path) {
		pendingActions.Store(string(m.ID), actions)
		return false
	}
	// A plain .go file: gopls answers it directly, so ask it here.
	go func() {
		raw, _ := p.ask(m.Method, json.RawMessage(m.Params))
		var got any
		_ = json.Unmarshal(raw, &got)
		list, _ := got.([]any)
		_ = p.editor.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": append(list, actions...)})
	}()
	return true
}

// withRefactors adds the code actions vuka worked out for the editor's
// request id to gopls's answer v.
func withRefactors(v any, id json.RawMessage) any {
	got, ok := pendingActions.LoadAndDelete(string(id))
	if !ok {
		return v
	}
	list, _ := v.([]any)
	return append(list, got.([]any)...)
}

// refactorReply takes the editor's answers to the proxy's own requests
// (workspace/applyEdit); it reports whether the message was one.
func (p *proxy) refactorReply(m *rpcMsg) bool {
	ch, ok := applies.LoadAndDelete(string(m.ID))
	if !ok {
		return false
	}
	ch.(chan rpcMsg) <- *m
	return true
}

// applyEdit asks the editor to apply a workspace edit and waits for its
// answer.
func (p *proxy) applyEdit(label string, edit map[string]any) error {
	id := strconv.Quote("vuka-apply:" + strconv.FormatInt(applySeq.Add(1), 10))
	ch := make(chan rpcMsg, 1)
	applies.Store(id, ch)
	_ = p.editor.send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": "workspace/applyEdit",
		"params": map[string]any{"label": label, "edit": edit}})
	select {
	case m := <-ch:
		var res struct {
			Applied       bool   `json:"applied"`
			FailureReason string `json:"failureReason"`
		}
		_ = json.Unmarshal(m.Result, &res)
		if len(m.Error) > 0 || !res.Applied {
			return errors.New("the editor didn't apply the edit " + res.FailureReason)
		}
		return nil
	case <-time.After(applyWait):
		applies.Delete(id)
		return errors.New("the editor didn't answer workspace/applyEdit")
	}
}

// buffer is a file's text: the editor's buffer, else the file on disk.
func (p *proxy) buffer(path string) ([]byte, bool) {
	p.mu.Lock()
	src, ok := p.bufs[path]
	p.mu.Unlock()
	if ok {
		return src, true
	}
	src, err := os.ReadFile(path)
	return src, err == nil
}

// readFunc reads files as the editor has them.
func (p *proxy) readFunc() load.ReadFunc {
	return func(path string) ([]byte, error) {
		p.mu.Lock()
		src, ok := p.bufs[path]
		p.mu.Unlock()
		if ok {
			return src, nil
		}
		return os.ReadFile(path)
	}
}

// addRefactorKinds adds vuka's code action kinds to gopls's capabilities.
func addRefactorKinds(caps map[string]any) {
	cap, ok := caps["codeActionProvider"].(map[string]any)
	if !ok {
		cap = map[string]any{"resolveProvider": true}
		caps["codeActionProvider"] = cap
	}
	kinds, _ := cap["codeActionKinds"].([]any)
	have := map[string]bool{}
	for _, k := range kinds {
		if s, ok := k.(string); ok {
			have[s] = true
		}
	}
	for _, k := range refactorKinds {
		if !have[k] {
			kinds = append(kinds, k)
		}
	}
	cap["codeActionKinds"] = kinds
	cap["resolveProvider"] = true
}

// runExtract runs vuka.extractComponent: it works out the edit, has the
// editor apply it, and answers the command.
func (p *proxy) runExtract(id json.RawMessage, args []json.RawMessage) {
	reply := func(err error) {
		msg := map[string]any{"jsonrpc": "2.0", "id": id, "result": nil}
		if err != nil {
			msg["error"] = map[string]any{"code": -32603, "message": err.Error()}
			delete(msg, "result")
		}
		_ = p.editor.send(msg)
	}
	var a struct {
		URI   string   `json:"uri"`
		Range lspRange `json:"range"`
		Name  string   `json:"name"`
	}
	if len(args) == 0 || json.Unmarshal(args[0], &a) != nil {
		reply(errors.New("vuka.extractComponent takes {uri, range, name}"))
		return
	}
	path := p.real(uriToPath(a.URI))
	src, ok := p.buffer(path)
	if !ok {
		reply(errors.New("can't read " + path))
		return
	}
	c, err := extractComponent(path, src, offsetOf(src, a.Range.Start), offsetOf(src, a.Range.End), a.Name, p.readFunc())
	if err != nil {
		reply(err)
		return
	}
	reply(p.applyEdit("Extract component", c.workspaceEdit(p.display)))
}

// resolveAction works out the edit of a code action whose edit was left
// for codeAction/resolve: data says which.
func (p *proxy) resolveAction(data map[string]any) map[string]any {
	path := p.real(uriToPath(stringOf(data["uri"])))
	src, ok := p.buffer(path)
	if !ok {
		return nil
	}
	c, err := resolveData(path, src, data, p.readFunc())
	if err != nil {
		p.logf("code action: %v", err)
		return nil
	}
	return c.workspaceEdit(p.display)
}

func stringOf(v any) string { s, _ := v.(string); return s }

// actionRequest is a code action request, as the actions see it.
type actionRequest struct {
	path       string
	src        []byte
	start, end int // the range, as offsets in src
	diags      []map[string]any
	only       []string
	read       load.ReadFunc
	uri        func(string) string // the path the editor knows a file by
}

// wants reports whether the request asks for actions of kind.
func (r actionRequest) wants(kind string) bool {
	if len(r.only) == 0 {
		return true
	}
	for _, o := range r.only {
		if kind == o || strings.HasPrefix(kind, o+".") {
			return true
		}
	}
	return false
}

// diag is the request's diagnostic of code over the range, nil when none.
func (r actionRequest) diag(code string) map[string]any {
	for _, d := range r.diags {
		if c, _ := d["code"].(string); c == code {
			return d
		}
	}
	return nil
}

// codeActions are vuka's actions for a request: refactors of a shape file,
// and fixes for the diagnostics.
func codeActions(r actionRequest) []any {
	out := []any{}
	add := func(a ...any) { out = append(out, a...) }
	switch {
	case strings.HasSuffix(r.path, ".vuka.html"):
		if r.wants("refactor.extract") {
			add(extractActions(r)...)
		}
		if r.wants("refactor.move") {
			add(moveActions(r)...)
		}
		if r.wants("quickfix") {
			add(templateFixes(r)...)
		}
	case strings.HasSuffix(r.path, ".go"):
		if r.wants("quickfix") || r.wants("refactor.move") {
			add(goFileFixes(r)...)
		}
	}
	return out
}

// codeAction is an action for the editor: its edit, or data for
// codeAction/resolve to work it out from, or a command.
type codeAction struct {
	Title       string           `json:"title"`
	Kind        string           `json:"kind"`
	Diagnostics []map[string]any `json:"diagnostics,omitempty"`
	IsPreferred bool             `json:"isPreferred,omitempty"`
	Edit        map[string]any   `json:"edit,omitempty"`
	Command     map[string]any   `json:"command,omitempty"`
	Data        map[string]any   `json:"data,omitempty"`
}

// lazyAction is an action whose edit needs the package's types: left for
// codeAction/resolve when the editor resolves, else worked out now.
func lazyAction(r actionRequest, a codeAction, data map[string]any) any {
	data["vuka"] = true
	data["uri"] = pathToURI(r.path)
	if editorResolves.Load() {
		a.Data = data
		return a
	}
	c, err := resolveData(r.path, r.src, data, r.read)
	if err != nil {
		return nil
	}
	a.Edit = c.workspaceEdit(r.uri)
	return a
}

// resolveData works out a lazy action's change from its data.
func resolveData(path string, src []byte, data map[string]any, read load.ReadFunc) (change, error) {
	switch stringOf(data["action"]) {
	case "key":
		return keyFix(path, src, int(numberOf(data["at"])), read)
	}
	return nil, errors.New("unknown action " + stringOf(data["action"]))
}

func numberOf(v any) float64 { f, _ := v.(float64); return f }

// edit replaces [start, end) of a file's text.
type edit struct {
	start, end int
	text       string
}

// fileChange is what a refactor does to one file.
type fileChange struct {
	path   string
	old    []byte // the text edited; nil for a file it creates
	create bool
	remove bool
	edits  []edit // on old; a created file's text is its one edit's
}

// change is a refactor's edits, file by file.
type change []fileChange

// created is a change creating path with text.
func created(path, text string) fileChange {
	return fileChange{path: path, create: true, edits: []edit{{text: text}}}
}

// result is the text each file has once the change is applied; nil for a
// file it deletes.
func (c change) result() map[string][]byte {
	out := map[string][]byte{}
	for _, f := range c {
		switch {
		case f.remove:
			out[f.path] = nil
		case f.create:
			out[f.path] = []byte(f.edits[0].text)
		default:
			out[f.path] = applySpans(f.old, f.edits)
		}
	}
	return out
}

// applySpans applies non-overlapping edits to src.
func applySpans(src []byte, edits []edit) []byte {
	sorted := append([]edit(nil), edits...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].start < sorted[j].start })
	var b bytes.Buffer
	at := 0
	for _, e := range sorted {
		if e.start < at {
			continue // overlapping: the earlier wins
		}
		b.Write(src[at:e.start])
		b.WriteString(e.text)
		at = e.end
	}
	b.Write(src[at:])
	return b.Bytes()
}

// workspaceEdit is the change as an LSP WorkspaceEdit of document changes.
func (c change) workspaceEdit(display func(string) string) map[string]any {
	if display == nil {
		display = func(s string) string { return s }
	}
	var docs []any
	for _, f := range c {
		uri := pathToURI(display(f.path))
		switch {
		case f.remove:
			docs = append(docs, map[string]any{"kind": "delete", "uri": uri})
		case f.create:
			docs = append(docs, map[string]any{"kind": "create", "uri": uri, "options": map[string]any{"overwrite": false, "ignoreIfExists": true}})
			docs = append(docs, map[string]any{"textDocument": map[string]any{"uri": uri, "version": nil},
				"edits": []any{map[string]any{"range": lspRange{}, "newText": f.edits[0].text}}})
		default:
			edits := []any{}
			sorted := append([]edit(nil), f.edits...)
			sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].start < sorted[j].start })
			for _, e := range sorted {
				edits = append(edits, map[string]any{"range": lspRange{positionOf(f.old, e.start), positionOf(f.old, e.end)}, "newText": e.text})
			}
			docs = append(docs, map[string]any{"textDocument": map[string]any{"uri": uri, "version": nil}, "edits": edits})
		}
	}
	return map[string]any{"documentChanges": docs}
}

// The imports of a file (.go, .vuka or .vuka.html): its package clause and
// the import declarations after it, read line by line.

type refImport struct {
	name, path string
	start, end int // the spec's line, its newline included
}

type importSection struct {
	pkg      string
	pkgEnd   int // the end of the package clause's line, its newline included
	specs    []refImport
	groups   [][2]int // each import ( … ) declaration: its first line's start, its last line's end
	singles  [][2]int // each import "x" declaration's line
	end      int      // the end of the last import line; pkgEnd when there are none
	hasGroup bool
}

var (
	importPackage = regexp.MustCompile(`^\s*package\s+(\w+)`)
	importSingle  = regexp.MustCompile(`^\s*import\s+(?:([\w.]+)\s+)?("[^"]*"|` + "`[^`]*`" + `)`)
	importOpen    = regexp.MustCompile(`^\s*import\s*\(\s*(?://.*)?$`)
	importInGroup = regexp.MustCompile(`^\s*(?:([\w.]+)\s+)?("[^"]*"|` + "`[^`]*`" + `)`)
)

// readImports reads a file's package clause and imports.
func readImports(src []byte) importSection {
	var s importSection
	off, inGroup, groupStart, inComment := 0, false, 0, false
	for off < len(src) {
		end := bytes.IndexByte(src[off:], '\n')
		if end < 0 {
			end = len(src)
		} else {
			end += off + 1
		}
		line := string(src[off:end])
		t := strings.TrimSpace(line)
		switch {
		case inComment:
			if strings.Contains(t, "*/") {
				inComment = false
			}
		case strings.HasPrefix(t, "/*") && !strings.Contains(t, "*/"):
			inComment = true
		case inGroup:
			if t == ")" {
				s.groups = append(s.groups, [2]int{groupStart, end})
				s.end, inGroup = end, false
			} else if g := importInGroup.FindStringSubmatch(t); g != nil {
				if path, err := strconv.Unquote(g[2]); err == nil {
					s.specs = append(s.specs, refImport{name: g[1], path: path, start: off, end: end})
				}
			}
		case t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*"):
		case s.pkg == "":
			g := importPackage.FindStringSubmatch(line)
			if g == nil {
				s.end = s.pkgEnd
				return s
			}
			s.pkg, s.pkgEnd, s.end = g[1], end, end
		case importOpen.MatchString(line):
			inGroup, groupStart, s.hasGroup = true, off, true
		case importSingle.MatchString(line):
			g := importSingle.FindStringSubmatch(line)
			if path, err := strconv.Unquote(g[2]); err == nil {
				s.specs = append(s.specs, refImport{name: g[1], path: path, start: off, end: end})
				s.singles = append(s.singles, [2]int{off, end})
				s.end = end
			}
		default:
			return s
		}
		off = end
	}
	return s
}

// localName is the name an import is used by.
func (i refImport) localName(pkgName func(string) string) string {
	if i.name != "" {
		return i.name
	}
	if pkgName != nil {
		if n := pkgName(i.path); n != "" {
			return n
		}
	}
	return defaultImportName(i.path)
}

// defaultImportName is the name an import path's package goes by, guessed
// from the path: its last element, without a major version.
func defaultImportName(path string) string {
	elems := strings.Split(path, "/")
	name := elems[len(elems)-1]
	if len(elems) > 1 && regexp.MustCompile(`^v[0-9]+$`).MatchString(name) {
		name = elems[len(elems)-2]
	}
	name = strings.TrimPrefix(name, "go-")
	if i := strings.IndexAny(name, ".-"); i >= 0 {
		name = name[:i]
	}
	if strings.HasPrefix(name, "private_") {
		name = strings.TrimPrefix(name, "private_")
	}
	return name
}

// wantImport is an import a change adds.
type wantImport struct{ name, path string }

// importEdits are the edits to src's imports removing the specs at drop
// (their paths) and adding add, in the fewest lines changed.
func importEdits(src []byte, drop map[string]bool, add []wantImport) []edit {
	s := readImports(src)
	have := map[string]bool{}
	for _, sp := range s.specs {
		have[sp.path] = true
	}
	var adds []wantImport
	for _, a := range add {
		if !have[a.path] && !drop[a.path] {
			adds = append(adds, a)
			have[a.path] = true
		}
	}
	var edits []edit
	removed := map[int]bool{}
	for i, sp := range s.specs {
		if drop[sp.path] {
			removed[i] = true
		}
	}
	specLine := func(a wantImport) string {
		if a.name != "" && a.name != defaultImportName(a.path) {
			return a.name + " " + strconv.Quote(a.path)
		}
		return strconv.Quote(a.path)
	}
	// A group emptied goes whole; a single import dropped goes.
	for _, g := range s.groups {
		all, any := true, false
		for i, sp := range s.specs {
			if sp.start >= g[0] && sp.end <= g[1] {
				any = true
				all = all && removed[i]
			}
		}
		if any && all && !(len(adds) > 0 && g == s.groups[0]) {
			edits = append(edits, edit{g[0], g[1], ""})
			for i, sp := range s.specs {
				if sp.start >= g[0] && sp.end <= g[1] {
					delete(removed, i)
				}
			}
		}
	}
	for i := range removed {
		edits = append(edits, edit{s.specs[i].start, s.specs[i].end, ""})
	}
	if len(adds) == 0 {
		return edits
	}
	var lines strings.Builder
	for _, a := range adds {
		lines.WriteString("\t" + specLine(a) + "\n")
	}
	switch {
	case len(s.groups) > 0:
		closing := s.groups[0][1]
		at := bytes.LastIndexByte(src[:closing-1], '\n') + 1 // the ) line
		edits = append(edits, edit{at, at, lines.String()})
	case len(s.singles) > 0:
		first := s.singles[0]
		sp := specOn(s.specs, first[0])
		text := "import (\n"
		if !removed[sp] {
			text += "\t" + specLine(wantImport{s.specs[sp].name, s.specs[sp].path}) + "\n"
		}
		text += lines.String() + ")\n"
		// the single import's own removal, if any, is replaced by this
		var kept []edit
		for _, e := range edits {
			if e.start != first[0] {
				kept = append(kept, e)
			}
		}
		edits = append(kept, edit{first[0], first[1], text})
	default:
		text := "\n"
		if len(adds) == 1 {
			text += "import " + specLine(adds[0]) + "\n"
		} else {
			text += "import (\n" + lines.String() + ")\n"
		}
		edits = append(edits, edit{s.pkgEnd, s.pkgEnd, text})
	}
	return edits
}

func specOn(specs []refImport, start int) int {
	for i, sp := range specs {
		if sp.start == start {
			return i
		}
	}
	return -1
}

// usedNames are the identifiers text uses as a qualifier, name.X: the
// imports it needs.
func usedNames(text []byte) map[string]bool {
	used := map[string]bool{}
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(text))
	var sc scanner.Scanner
	sc.Init(file, text, func(token.Position, string) {}, 0)
	prev, prevLit := token.ILLEGAL, ""
	before := token.ILLEGAL
	for {
		_, t, lit := sc.Scan()
		if t == token.EOF {
			break
		}
		if t == token.PERIOD && prev == token.IDENT && before != token.PERIOD {
			used[prevLit] = true
		}
		before, prev, prevLit = prev, t, lit
	}
	return used
}

// unusedImports are the paths of src's imports text no longer uses, text
// being src's code outside its import section once a change is applied.
func unusedImports(src, text []byte, pkgName func(string) string) map[string]bool {
	s := readImports(src)
	used := usedNames(text)
	out := map[string]bool{}
	for _, sp := range s.specs {
		n := sp.localName(pkgName)
		if n == "_" || n == "." || sp.path == "C" {
			continue
		}
		if !used[n] {
			out[sp.path] = true
		}
	}
	return out
}

// pathPackageNames reads the package clause of an import path's directory
// in the module, for an import whose name isn't its last element.
func pathPackageNames(root, modPath string) func(string) string {
	return func(path string) string {
		rel, ok := strings.CutPrefix(path, modPath+"/")
		if !ok {
			return ""
		}
		dir := filepath.Join(root, filepath.FromSlash(rel))
		if d, ok := load.SourceDir(root, dir); ok {
			dir = d
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return ""
		}
		for _, e := range entries {
			n := e.Name()
			if e.IsDir() || !(strings.HasSuffix(n, ".go") || isVuka(n)) || strings.HasSuffix(n, "_test.go") {
				continue
			}
			src, err := os.ReadFile(filepath.Join(dir, n))
			if err != nil {
				continue
			}
			if s := readImports(src); s.pkg != "" {
				return s.pkg
			}
		}
		return ""
	}
}
