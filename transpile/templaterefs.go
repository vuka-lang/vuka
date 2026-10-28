package transpile

import (
	"go/token"
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
	root := p.trees[0].root
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
	return refs
}
