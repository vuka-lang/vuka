package main

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// A value enum's variant methods, func (PasswordError.Mismatch) Error()
// string, lower to functions that one method on the enum switches between
// (transpile's enummethods.go). Hover on a variant method's declaration
// says so, and definition from a call of the enum's method, which gopls
// answers with the generated switch, goes to every declaration of it.

// variantMethodAt is the variant method whose declaration, from func to
// its name, holds off in f's text.
func (f *vfile) variantMethodAt(off int) (transpile.VariantMethod, bool) {
	from := f.fromOff(off)
	for _, vm := range f.variants {
		if from >= vm.Header[0] && from < vm.Name[1] && !f.goneAt(vm.Header[0]) {
			return vm, true
		}
	}
	return transpile.VariantMethod{}, false
}

// variantMethodRequest answers hover on a variant method's declaration;
// nil when off isn't on one. p.mu must be held.
func (p *proxy) variantMethodRequest(method string, f *vfile, off int) func() any {
	if method != "textDocument/hover" {
		return nil
	}
	vm, ok := f.variantMethodAt(off)
	if !ok {
		return nil
	}
	text := f.text()
	header := strings.Join(strings.Fields(string(f.from[vm.Header[0]:vm.Header[1]])), " ")
	start, end := f.curOff(vm.Name[0]), f.curOff(vm.Name[1])
	return func() any { return markdownHover(variantMethodDoc(vm, header), text, start, end) }
}

// variantMethodDoc is a variant method's hover.
func variantMethodDoc(vm transpile.VariantMethod, header string) string {
	doc := "```go\n" + header + "\n```\n\n"
	if vm.Variant == "" {
		return doc + "The default of `" + vm.Enum + "." + vm.Method + "`: `" + vm.Enum + "` is a value enum, and its one `" + vm.Method +
			"` method calls this for the variants without a " + vm.Method + " of their own, and for a value that is no variant."
	}
	return doc + "`" + vm.Enum + "." + vm.Variant + "`'s `" + vm.Method + "`: `" + vm.Enum + "` is a value enum, and its one `" + vm.Method +
		"` method switches on the value and calls this for `" + vm.Enum + "." + vm.Variant + "`."
}

// withVariantDecls replaces a definition at the switch the variant methods
// of one enum and name lower to with every declaration of the method;
// asked on a declaration's own name (origin, off), the answer is kept.
func (p *proxy) withVariantDecls(v any, origin *vfile, off int) any {
	list, _ := v.([]any)
	single := false
	if m, ok := v.(map[string]any); ok {
		list, single = []any{m}, true
	}
	if len(list) == 0 {
		return v
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	files := map[string]*vfile{}
	for _, f := range p.virtual {
		if f.m != nil && len(f.variants) > 0 {
			files[f.source] = f
		}
	}
	if len(files) == 0 {
		return v
	}
	fileOf := func(path string) *vfile {
		if f := files[path]; f != nil {
			return f
		}
		for real, shown := range p.shown {
			if shown == path {
				return files[real]
			}
		}
		return nil
	}
	var out []any
	changed := false
	for _, loc := range list {
		m, ok := loc.(map[string]any)
		uri, _ := m["uri"].(string)
		r, ok2 := toRange(m["range"])
		f := fileOf(uriToPath(uri))
		if !ok || !ok2 || f == nil {
			out = append(out, loc)
			continue
		}
		at := f.fromOff(offsetOf(f.text(), r.Start))
		var set *transpile.VariantMethod
		for i := range f.variants {
			if vm := f.variants[i]; vm.Dispatch == at && vm.Name[0] == at {
				set = &f.variants[i]
				break
			}
		}
		if set == nil || origin != nil && origin.source == f.source && origin.fromOff(off) >= set.Name[0] && origin.fromOff(off) <= set.Name[1] {
			out = append(out, loc)
			continue
		}
		changed = true
		dir := filepath.Dir(f.source)
		var sources []string
		for src := range files {
			if filepath.Dir(src) == dir {
				sources = append(sources, src)
			}
		}
		sort.Strings(sources)
		for _, src := range sources {
			g := files[src]
			for _, vm := range g.variants {
				if vm.Enum != set.Enum || vm.Method != set.Method {
					continue
				}
				text := g.text()
				out = append(out, map[string]any{"uri": pathToURI(shownPath(p.shown, g.source)),
					"range": lspRange{positionOf(text, g.curOff(vm.Name[0])), positionOf(text, g.curOff(vm.Name[1]))}})
			}
		}
	}
	if !changed {
		return v
	}
	if single && len(out) == 1 {
		return out[0]
	}
	return out
}

// shownPath is real as the editor knows it (proxy.display, p.mu held).
func shownPath(shown map[string]string, real string) string {
	if s, ok := shown[real]; ok {
		return s
	}
	return real
}
