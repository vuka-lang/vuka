package main

import (
	"bytes"
	"fmt"
	"go/format"
	"go/scanner"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/transpile"
)

// UIPath is the UI library the runtime's UI layer moved to in v0.10.0.
const UIPath = "github.com/vuka-lang/ui"

// uiNames are the runtime's names that moved to package ui.
var uiNames = names(`Assign AttachLive Attr Boundary BuildTree Builder Child Component
	ComponentNode CopyLive El Element ErrorBoundary Event EventHandler EventName F Fragment Frame
	Group HTMLRenderer Handler KeyText Live LiveHost LiveInstance MountLive NewHTMLRenderer Node
	NodeFunc Nodes On OpaqueTree RawHTML Renderer RootAttrs Safe SettleState Stateful String Style
	Text TextNode TrackedState Tree TreeHost TreeList TreeRef Try TryNode Void Walk WithLiveHost Write`)

// templxNames are the runtime's .templ registry names, now package templx's.
var templxNames = map[string]string{"TemplComponent": "Component", "RegisterTempl": "Register", "TemplComponents": "Components"}

func names(s string) map[string]bool {
	m := map[string]bool{}
	for _, n := range strings.Fields(s) {
		m[n] = true
	}
	return m
}

// fixUI moves code to github.com/vuka-lang/ui (v0.10.0): the runtime's UI
// names, the live, term and templx packages, and an import of ui in every
// .vuka file with JSX.
func fixUI(m *fixModule) ([]fixChange, error) {
	var changes []fixChange
	err := walkModule(m.root, func(path string) error {
		vuka := strings.HasSuffix(path, ".vuka")
		if !vuka && (!strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_vuka.go") || strings.HasSuffix(path, "_vuka_test.go")) {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out, what := fixUISource(src, vuka)
		if what == nil {
			return nil
		}
		rel, _ := filepath.Rel(m.root, path)
		changes = append(changes, fixChange{
			what:  "in " + rel + ", " + strings.Join(what, "; "),
			apply: func() error { return os.WriteFile(path, out, 0o644) },
		})
		return nil
	})
	if err != nil || len(changes) == 0 || requiresUI(m.root) {
		return changes, err
	}
	return append(changes, fixChange{
		what: "require " + UIPath,
		apply: func() error {
			c := exec.Command("go", "get", UIPath+"@latest")
			c.Dir = m.root
			if out, err := c.CombinedOutput(); err != nil {
				return fmt.Errorf("go get: %v\n%s", err, out)
			}
			return nil
		},
	}), nil
}

var requireUI = regexp.MustCompile(`(?m)^\s*(?:require\s+)?` + regexp.QuoteMeta(UIPath) + `\s+v`)

func requiresUI(root string) bool {
	mod, _ := os.ReadFile(filepath.Join(root, "go.mod"))
	return requireUI.Match(mod)
}

type srcEdit struct {
	start, end int
	text       string
}

type importLine struct {
	name, path string
	start, end int // the spec, its line's end included
}

// fixUISource rewrites one file; what says what changed, nil for nothing.
func fixUISource(src []byte, vuka bool) ([]byte, []string) {
	toks, file := uiTokens(src)
	imps := uiImports(src, toks, file)
	rt := ""
	for _, imp := range imps {
		if imp.path == transpile.RuntimePath {
			rt = imp.name
			if rt == "" {
				rt = "vuka"
			}
		}
	}
	if rt == "" && vuka {
		rt = "vuka" // .vuka files get the runtime without importing it
	}
	var edits []srcEdit
	var what []string
	moved := map[string]bool{}
	useUI, useTemplx, rtLeft := false, false, false
	for i := 0; i+2 < len(toks); i++ {
		t := toks[i]
		if t.tok != token.IDENT || t.lit != rt || rt == "" || toks[i+1].tok != token.PERIOD || toks[i+2].tok != token.IDENT {
			continue
		}
		if i > 0 && toks[i-1].tok == token.PERIOD {
			continue
		}
		name := toks[i+2].lit
		switch {
		case uiNames[name]:
			edits = append(edits, srcEdit{t.off, t.off + len(rt), "ui"})
			moved[rt+"."+name+" → ui."+name] = true
			useUI = true
		case templxNames[name] != "":
			edits = append(edits, srcEdit{t.off, toks[i+2].off + len(name), "templx." + templxNames[name]})
			moved[rt+"."+name+" → templx."+templxNames[name]] = true
			useTemplx = true
		default:
			rtLeft = true
		}
	}
	for m := range moved {
		what = append(what, m)
	}
	slices.Sort(what)
	hasUI, hasTemplx := false, false
	for _, imp := range imps {
		for _, sub := range []string{"live", "term", "templx"} {
			old := transpile.RuntimePath + "/" + sub
			if imp.path == old || strings.HasPrefix(imp.path, old+"/") {
				np := UIPath + strings.TrimPrefix(imp.path, transpile.RuntimePath)
				q := bytes.Index(src[imp.start:imp.end], []byte(strconv.Quote(imp.path)))
				edits = append(edits, srcEdit{imp.start + q, imp.start + q + len(strconv.Quote(imp.path)), strconv.Quote(np)})
				what = append(what, imp.path+" → "+np)
				if np == UIPath+"/templx" {
					hasTemplx = true
				}
			}
		}
		switch imp.path {
		case UIPath:
			hasUI = true
		case UIPath + "/templx":
			hasTemplx = true
		case transpile.RuntimePath:
			if (useUI || useTemplx) && !rtLeft {
				edits = append(edits, srcEdit{imp.start, imp.end, ""})
				what = append(what, "drop the unused import of "+transpile.RuntimePath)
			}
		}
	}
	if vuka && !hasUI && transpile.HasJSX(src) {
		useUI = true
	}
	var add []string
	if useUI && !hasUI {
		add = append(add, UIPath)
	}
	if useTemplx && !hasTemplx {
		add = append(add, UIPath+"/templx")
	}
	if len(what) == 0 && len(add) == 0 {
		return src, nil
	}
	for _, p := range add {
		what = append(what, "import "+p)
	}
	var b bytes.Buffer
	last := 0
	slices.SortFunc(edits, func(a, b srcEdit) int { return a.start - b.start })
	for _, e := range edits {
		b.Write(src[last:e.start])
		b.WriteString(e.text)
		last = e.end
	}
	b.Write(src[last:])
	out := addUIImports(b.Bytes(), add)
	if !vuka { // a .vuka file keeps its layout; vuka fmt formats it
		if f, err := format.Source(out); err == nil {
			out = f
		}
	}
	return out, what
}

type uiTok struct {
	off int
	tok token.Token
	lit string
}

func uiTokens(src []byte) ([]uiTok, *token.File) {
	file := token.NewFileSet().AddFile("", -1, len(src))
	var s scanner.Scanner
	s.Init(file, src, func(token.Position, string) {}, 0)
	var out []uiTok
	for {
		pos, t, lit := s.Scan()
		if t == token.EOF {
			return out, file
		}
		if t == token.SEMICOLON && lit == "\n" {
			continue
		}
		out = append(out, uiTok{file.Offset(pos), t, lit})
	}
}

// uiImports are a file's import specs, each with the line it is on.
func uiImports(src []byte, toks []uiTok, file *token.File) []importLine {
	var out []importLine
	lineSpan := func(start, end int) (int, int) {
		for start > 0 && src[start-1] != '\n' {
			start--
		}
		for end < len(src) && src[end] != '\n' {
			end++
		}
		if end < len(src) {
			end++
		}
		return start, end
	}
	for i := 0; i < len(toks); i++ {
		switch toks[i].tok {
		case token.FUNC, token.TYPE, token.VAR, token.CONST:
			return out
		case token.IMPORT:
		default:
			continue
		}
		grouped := i+1 < len(toks) && toks[i+1].tok == token.LPAREN
		for j := i + 1; j < len(toks); j++ {
			t := toks[j]
			if t.tok == token.RPAREN || !grouped && j > i+2 {
				i = j
				break
			}
			if t.tok != token.STRING {
				continue
			}
			p, _ := strconv.Unquote(t.lit)
			name, from := "", t.off
			if toks[j-1].tok == token.IDENT || toks[j-1].tok == token.PERIOD {
				name, from = toks[j-1].lit, toks[j-1].off
				if toks[j-1].tok == token.PERIOD {
					name = "."
				}
			}
			s, e := from, t.off+len(t.lit)
			if grouped {
				s, e = lineSpan(from, e)
			} else {
				s, e = lineSpan(toks[i].off, e)
			}
			out = append(out, importLine{name, p, s, e})
			if !grouped {
				i = j
				break
			}
		}
	}
	return out
}

// addUIImports adds imports of paths after the file's last import, or after
// its package clause.
func addUIImports(src []byte, paths []string) []byte {
	if len(paths) == 0 {
		return src
	}
	toks, file := uiTokens(src)
	imps := uiImports(src, toks, file)
	var lines strings.Builder
	for _, p := range paths {
		lines.WriteString("\t" + strconv.Quote(p) + "\n")
	}
	if len(imps) > 0 {
		last := imps[len(imps)-1]
		if bytes.HasPrefix(bytes.TrimSpace(src[last.start:last.end]), []byte("import ")) {
			// One ungrouped import: group it with the new ones.
			spec := bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(src[last.start:last.end]), []byte("import")))
			sep := ""
			if stdImport(last.path) {
				sep = "\n"
			}
			return concat(src[:last.start], "import (\n\t"+string(spec)+"\n"+sep+lines.String()+")\n", src[last.end:])
		}
		sep := ""
		if stdImport(last.path) {
			sep = "\n"
		}
		return concat(src[:last.end], sep+lines.String(), src[last.end:])
	}
	for i, t := range toks {
		if t.tok == token.PACKAGE && i+1 < len(toks) {
			end := toks[i+1].off + len(toks[i+1].lit)
			if len(paths) == 1 {
				return concat(src[:end], "\n\nimport "+strconv.Quote(paths[0]), src[end:])
			}
			return concat(src[:end], "\n\nimport (\n"+lines.String()+")", src[end:])
		}
	}
	return src
}

func stdImport(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

func concat(a []byte, mid string, b []byte) []byte {
	out := append(append([]byte{}, a...), mid...)
	return append(out, b...)
}
