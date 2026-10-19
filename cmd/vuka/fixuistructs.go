package main

import (
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// uiKit is each component of the ui kit and its former props type: the
// kit's components are structs, each a ui.Node, ui.Card{…} where a function
// took its props, ui.Card(ui.CardProps{…}).
var uiKit = map[string]string{
	"Alert": "AlertProps", "Badge": "BadgeProps", "Button": "ButtonProps", "Submit": "ButtonProps",
	"Card": "CardProps", "DataTable": "DataTableProps", "Form": "FormProps", "Image": "ImageProps",
	"Field": "FieldProps", "Input": "FieldProps", "Password": "FieldProps", "Textarea": "FieldProps",
	"Select": "FieldProps", "Checkbox": "FieldProps", "Modal": "ModalProps", "Table": "TableProps",
	"Tabs": "TabsProps", "Tile": "TileProps", "Toast": "ToastProps", "ToastContainer": "ToastContainerProps",
}

// fixUIStructs rewrites calls of the ui kit's components as functions into
// their struct form, once the module's ui is one whose components are
// structs.
func fixUIStructs(m *fixModule) ([]fixChange, error) {
	if !uiKitStructs(m.root) {
		return nil, nil
	}
	var changes []fixChange
	err := walkModule(m.root, func(path string) error {
		if !strings.HasSuffix(path, ".vuka") && (!strings.HasSuffix(path, ".go") || generatedGo(path)) {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out, what := fixUIStructsSource(src)
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
	return changes, err
}

// uiKitStructs reports whether the ui the module at root builds with
// declares its components as structs.
func uiKitStructs(root string) bool {
	c := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", UIPath)
	c.Dir = root
	out, err := c.Output()
	if err != nil {
		return false
	}
	src, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(out)), "card.go"))
	return err == nil && strings.Contains(string(src), "\ntype Card struct {")
}

func generatedGo(path string) bool {
	for _, s := range []string{"_vuka.go", "_vuka_test.go", "_html.go", "_html_test.go"} {
		if strings.HasSuffix(path, s) {
			return true
		}
	}
	return false
}

// fixUIStructsSource rewrites one file: ui.X(ui.XProps{…}) is ui.X{…}, and
// ui.Table(ui.TableProps[T]{…}) ui.Table[T]{…}. (ui.X(p) with a props value
// p still compiles: the props types are aliases of the components, so it is
// a conversion, the component itself; a generic one, ui.Table(t), needs its
// type argument, ui.Table[Row](t), which the source doesn't say.) what says
// what changed, nil for nothing.
func fixUIStructsSource(src []byte) ([]byte, []string) {
	toks, file := uiTokens(src)
	q := ""
	for _, imp := range uiImports(src, toks, file) {
		if imp.path == UIPath {
			q = imp.name
			if q == "" {
				q = "ui"
			}
		}
	}
	if q == "" || q == "." || q == "_" {
		return src, nil
	}
	var edits []srcEdit
	seen := map[string]bool{}
	for i := 0; i+3 < len(toks); i++ {
		if toks[i].tok != token.IDENT || toks[i].lit != q || toks[i+1].tok != token.PERIOD ||
			toks[i+2].tok != token.IDENT || toks[i+3].tok != token.LPAREN || i > 0 && toks[i-1].tok == token.PERIOD {
			continue
		}
		name := toks[i+2].lit
		props, ok := uiKit[name]
		if !ok {
			continue
		}
		open := i + 3
		close, args := kitClose(toks, open)
		if close < 0 || args != 1 || close == open+1 {
			continue
		}
		if open+3 < close && toks[open+1].lit == q && toks[open+2].tok == token.PERIOD && toks[open+3].lit == props {
			// ui.X(ui.XProps[T]{…}): the literal is the call's one argument.
			j := open + 4
			if j < close && toks[j].tok == token.LBRACK {
				j, _ = kitClose(toks, j)
				j++
			}
			if j < close && toks[j].tok == token.LBRACE {
				end, _ := kitClose(toks, j)
				if end == close-1 || end == close-2 && toks[close-1].tok == token.COMMA {
					edits = append(edits,
						srcEdit{toks[open].off, toks[open+3].off + len(props), ""},
						srcEdit{toks[end].off + 1, toks[close].off + 1, ""})
					seen[q+"."+name+"("+q+"."+props+"{…}) → "+q+"."+name+"{…}"] = true
					continue
				}
			}
		}
	}
	if len(edits) == 0 {
		return src, nil
	}
	var what []string
	for w := range seen {
		what = append(what, w)
	}
	slices.Sort(what)
	slices.SortStableFunc(edits, func(a, b srcEdit) int { return a.start - b.start })
	var b strings.Builder
	last := 0
	for _, e := range edits {
		b.Write(src[last:e.start])
		b.WriteString(e.text)
		last = e.end
	}
	b.Write(src[last:])
	return []byte(b.String()), what
}

// kitClose is the index of the bracket closing toks[open] — a (, [ or {
// — and how many arguments or elements are between them (top-level commas
// plus one; a trailing comma adds none).
func kitClose(toks []uiTok, open int) (int, int) {
	depth, args := 0, 1
	for j := open; j < len(toks); j++ {
		switch toks[j].tok {
		case token.LPAREN, token.LBRACK, token.LBRACE:
			depth++
		case token.RPAREN, token.RBRACK, token.RBRACE:
			depth--
			if depth == 0 {
				if toks[j-1].tok == token.COMMA {
					args--
				}
				return j, args
			}
		case token.COMMA:
			if depth == 1 {
				args++
			}
		}
	}
	return -1, 0
}
