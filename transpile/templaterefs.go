package transpile

import (
	"go/token"
	"sort"
	"strings"
)

// TemplateRef describes a named element of a template's markup.
type TemplateRef struct {
	Name, Tag         string
	Conditional, Loop bool
}

// TemplateRefs reads refs using the template parser, including the control
// flow around each element. Malformed markup returns no refs.
func TemplateRefs(markup string) (refs []TemplateRef) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(jsxError); !ok {
				panic(r)
			}
			refs = nil
		}
	}()
	src := []byte("<>" + markup + "</>")
	var errs ErrorList
	f := &fileState{src: src, html: &htmlFile{recv: "p", src: src, lines: newLineIndex(src)}}
	p := &jsxParser{f: f, src: src, errs: &errs}
	p.tree(0)
	if len(errs) > 0 {
		return nil
	}
	root := p.trees[len(p.trees)-1].root
	var walk func([]jsxNode, bool, bool)
	walk = func(ns []jsxNode, conditional, loop bool) {
		for _, n := range ns {
			switch n := n.(type) {
			case *jsxElem:
				conditional := conditional || strings.HasPrefix(n.tag, "slot:")
				for _, a := range n.attrs {
					if a.name != "ref" || a.kind != 's' {
						continue
					}
					name := a.val
					refs = append(refs, TemplateRef{Name: name, Tag: n.tag, Conditional: conditional, Loop: loop})
				}
				walk(n.kids, conditional, loop)
			case *jsxBlock:
				if n.stmt {
					continue
				}
				isLoop := false
				if len(n.heads) > 0 {
					text := strings.TrimSpace(string(src[n.heads[0].start:n.heads[0].end]))
					isLoop = strings.HasPrefix(text, token.FOR.String()+" ")
				}
				for _, body := range n.bodies {
					walk(body, conditional || !isLoop, loop || isLoop)
				}
				for _, body := range n.cases {
					walk(body, true, loop)
				}
			}
		}
	}
	walk([]jsxNode{root}, false, false)
	for _, t := range p.trees {
		if t.fn {
			// A markup func's body renders wherever, and however often, it is called.
			walk([]jsxNode{t.root}, true, true)
		}
	}
	return refs
}

// MarkupFunc is a markup func a template's markup binds by name, row :=
// func(o Order) { … }: offsets into the markup.
type MarkupFunc struct {
	Name               string
	NameStart, NameEnd int // the bound name
	End                int // after the body's }
}

// MarkupFuncs reads the markup funcs markup binds by name, for the
// editor's outline. Malformed markup returns none.
func MarkupFuncs(markup []byte) (fns []MarkupFunc) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(jsxError); !ok {
				panic(r)
			}
			fns = nil
		}
	}()
	src := append(append([]byte("<>\n"), markup...), "\n</>"...) // a statement starts its line
	var errs ErrorList
	f := &fileState{src: src, html: &htmlFile{recv: "p", src: src, lines: newLineIndex(src)}}
	p := &jsxParser{f: f, src: src, errs: &errs}
	p.tree(0)
	for _, t := range p.trees {
		if !t.fn {
			continue
		}
		// The literal is the value of name := or name =.
		i := t.lit
		for i > 0 && (src[i-1] == ' ' || src[i-1] == '\t') {
			i--
		}
		if i > 0 && src[i-1] == '=' {
			i--
			if i > 0 && src[i-1] == ':' {
				i--
			}
		} else {
			continue // a component's attribute's: it binds no name
		}
		for i > 0 && (src[i-1] == ' ' || src[i-1] == '\t') {
			i--
		}
		end := i
		for i > 0 && isIdentByte(src[i-1]) {
			i--
		}
		if i == end {
			continue
		}
		fns = append(fns, MarkupFunc{Name: string(src[i:end]), NameStart: i - 3, NameEnd: end - 3, End: t.end - 3})
	}
	sort.Slice(fns, func(a, b int) bool { return fns[a].NameStart < fns[b].NameStart })
	return fns
}
