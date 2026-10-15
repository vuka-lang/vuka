package tsprops

import (
	"regexp"
	"strings"
)

var slotRe = regexp.MustCompile(`<slot(\s*/?>|\s+(?:[^>]*\s)?name\s*=\s*["']default["'])|<slot\s+[^>]*?/?>`)
var namedSlotRe = regexp.MustCompile(`<slot\s+[^>]*\bname\s*=\s*["']([^"']+)["']`)

// vue reads a single-file component: defineProps (with withDefaults or a
// destructuring's defaults), defineModel and defineEmits, in its
// <script setup lang="ts">.
func (r *reader) vue(c *Component) error {
	if tpl := templateOf(r.src); hasDefaultSlot(tpl) {
		c.Children = true
	}
	if i, _ := r.findCall(0, "defineProps"); i >= 0 {
		t, after, err := r.typeArg(i)
		if err != nil {
			return err
		}
		if t == nil {
			return r.errAt(r.tok(i).off, "defineProps declares runtime props; vuka reads typed props: defineProps<{ … }>()")
		}
		props, err := r.propsOf(t, r.tok(i).off)
		if err != nil {
			return err
		}
		defaults := map[string]bool{}
		// withDefaults(defineProps<…>(), { a: 1 })
		if i >= 2 && r.isPunct(i-1, "(") && r.isIdent(i-2, "withDefaults") {
			close := matching(r.toks, i-1)
			if end := matching(r.toks, after); end > 0 && r.isPunct(end+1, ",") && r.isPunct(end+2, "{") && close > end {
				defaults = r.objectKeys(end + 2)
			}
		}
		// const { a = 1 } = defineProps<…>()
		if i >= 2 && r.isPunct(i-1, "=") && r.isPunct(i-2, "}") {
			for k := i - 2; k >= 0; k-- {
				if r.isPunct(k, "{") && matching(r.toks, k) == i-2 {
					names, _ := r.destructured(k)
					for _, n := range names {
						if n.hasDefault {
							defaults[n.name] = true
						}
					}
					break
				}
			}
		}
		for _, p := range props {
			if defaults[p.Name] {
				p.Optional = true
			}
		}
		c.Props = append(c.Props, props...)
	}
	for from := 0; ; {
		i, _ := r.findCall(from, "defineModel")
		if i < 0 {
			break
		}
		from = i + 1
		t, after, err := r.typeArg(i)
		if err != nil {
			return err
		}
		name, required := "modelValue", false
		if r.isPunct(after, "(") {
			k := after + 1
			if r.tok(k).kind == tString {
				name = r.tok(k).text
				k++
				r.isPunct(k, ",")
				if r.isPunct(k, ",") {
					k++
				}
			}
			if r.isPunct(k, "{") {
				end := matching(r.toks, k)
				for j := k; j < end; j++ {
					if r.isIdent(j, "required") && r.isPunct(j+1, ":") && r.isIdent(j+2, "true") {
						required = true
					}
					if r.isIdent(j, "default") && r.isPunct(j+1, ":") {
						required = false
					}
				}
			}
		}
		if t == nil {
			return r.errAt(r.tok(i).off, "defineModel(%q) needs its type: defineModel<T>(…)", name)
		}
		c.Props = append(c.Props, &Prop{Name: name, Type: t, Optional: !required, Model: true, Line: r.line(r.tok(i).off)})
	}
	if i, _ := r.findCall(0, "defineEmits"); i >= 0 {
		t, after, err := r.typeArg(i)
		if err != nil {
			return err
		}
		if t == nil {
			// defineEmits(['select', 'close']): names without argument types.
			if r.isPunct(after, "(") && r.isPunct(after+1, "[") {
				end := matching(r.toks, after+1)
				for k := after + 2; k < end; k++ {
					if s := r.tok(k); s.kind == tString && !strings.HasPrefix(s.text, "update:") {
						c.Props = append(c.Props, &Prop{Name: emitProp(s.text), Optional: true, Event: true, Line: r.line(s.off),
							Type: &Type{Kind: Func, Params: []*Param{{Name: "payload", Type: &Type{Kind: Any, Text: "any"}, Optional: true}}, Text: "(payload?: any) => void"}})
					}
				}
				return nil
			}
			return r.errAt(r.tok(i).off, "defineEmits declares runtime emits; vuka reads typed emits: defineEmits<{ select: [day: string] }>()")
		}
		et := r.resolve(t, 0)
		if et.Kind != Object {
			return r.errAt(r.tok(i).off, "defineEmits' type %s isn't an object type", t.String())
		}
		for _, f := range et.Fields {
			switch {
			case f.Name == "(call)": // (e: 'select', day: string): void
				ps := f.Type.Params
				if len(ps) == 0 || ps[0].Type.Kind != StrLit {
					return r.errAt(f.Off, "an emit's call signature names its event with a string literal first: (e: 'select', …): void")
				}
				if strings.HasPrefix(ps[0].Type.Lit, "update:") {
					continue
				}
				c.Props = append(c.Props, &Prop{Name: emitProp(ps[0].Type.Lit), Optional: true, Event: true, Line: r.line(f.Off),
					Type: &Type{Kind: Func, Params: ps[1:], Text: f.Type.Text}})
			case strings.HasPrefix(f.Name, "update:"):
			default: // select: [day: string]
				ft := &Type{Kind: Func, Text: f.Type.String()}
				switch f.Type.Kind {
				case Tuple:
					for k, e := range f.Type.Union {
						ft.Params = append(ft.Params, &Param{Name: tupleName(f.Type.Text, k), Type: e})
					}
				case Func:
					ft = f.Type
				default:
					return r.errAt(f.Off, "emit %s's type %s isn't a tuple of its arguments: %s: [day: string]", f.Name, f.Type.String(), f.Name)
				}
				c.Props = append(c.Props, &Prop{Name: emitProp(f.Name), Optional: true, Event: true, Line: r.line(f.Off), Type: ft})
			}
		}
	}
	return nil
}

// tupleName is the name of a named tuple's k-th member ([day: string]), else
// arg<k>.
func tupleName(text string, k int) string {
	inner := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(text), "["), "]")
	parts := splitTop(inner)
	if k < len(parts) {
		if name, _, ok := strings.Cut(parts[k], ":"); ok {
			name = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(name), "..."), "?"))
			if name != "" && !strings.ContainsAny(name, "{[(<'\" ") {
				return name
			}
		}
	}
	return "arg" + string(rune('0'+k))
}

func splitTop(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, c := range s {
		switch c {
		case '(', '[', '{', '<':
			depth++
		case ')', ']', '}', '>':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// emitProp is the listener prop of a Vue emit: select → onSelect,
// row-click → onRowClick.
func emitProp(name string) string {
	var b strings.Builder
	b.WriteString("on")
	up := true
	for _, ch := range name {
		if ch == '-' || ch == '_' || ch == ':' {
			up = true
			continue
		}
		if up {
			b.WriteString(strings.ToUpper(string(ch)))
			up = false
			continue
		}
		b.WriteRune(ch)
	}
	return b.String()
}

var templateRe = regexp.MustCompile(`(?is)<template\b[^>]*>(.*)</template\s*>`)

func templateOf(src string) string {
	masked := scriptRe.ReplaceAllString(src, "")
	if m := templateRe.FindStringSubmatch(masked); m != nil {
		return m[1]
	}
	return ""
}

func hasDefaultSlot(tpl string) bool {
	for _, m := range regexp.MustCompile(`<slot\b[^>]*>`).FindAllString(tpl, -1) {
		if n := namedSlotRe.FindStringSubmatch(m); n == nil || n[1] == "default" {
			return true
		}
	}
	return false
}

// react reads a React component's props: its default export's first
// parameter, typed (function, arrow, forwardRef, memo), or a React.FC<P>
// annotation.
func (r *reader) react(c *Component) error {
	name, at := r.defaultExport()
	if at < 0 {
		return r.errAt(0, "no default export: an island component is its file's default export")
	}
	t, off, err := r.componentProps(name, at)
	if err != nil {
		return err
	}
	if t == nil {
		c.Props = nil
		return nil
	}
	props, err := r.propsOf(t, off)
	if err != nil {
		return err
	}
	c.Props = props
	r.applyDefaults(c.Props)
	return nil
}

// defaultExport is what export default names: an identifier, or the index
// of the expression after it.
func (r *reader) defaultExport() (string, int) {
	for i := 0; i+1 < len(r.toks); i++ {
		if r.isIdent(i, "export") && r.isIdent(i+1, "default") {
			j := i + 2
			if r.tok(j).kind == tIdent && (r.isPunct(j+1, ";") || r.tok(j+1).nl || r.tok(j+1).kind == tEOF) {
				return r.tok(j).text, j
			}
			return "", j
		}
	}
	return "", -1
}

// destructuredDefaults are the names a component's props destructuring
// gives defaults, found by componentProps.
func (r *reader) applyDefaults(props []*Prop) {
	for _, p := range props {
		if r.defaults[p.Name] {
			p.Optional = true
		}
	}
}

// componentProps finds the props type of the component an export names: an
// identifier declared in the file (function or const), or an expression at
// i. It returns nil when the component takes no props.
func (r *reader) componentProps(name string, i int) (*Type, int, error) {
	if name != "" {
		for k := 0; k < len(r.toks); k++ {
			if r.isIdent(k, "function") && r.isIdent(k+1, name) {
				return r.funcProps(k+2, 0)
			}
			if (r.isIdent(k, "const") || r.isIdent(k, "let") || r.isIdent(k, "var")) && r.isIdent(k+1, name) {
				j := k + 2
				if r.isPunct(j, ":") { // const X: React.FC<P> = …
					p := r.parserAt(j + 1)
					ann := p.typ()
					if ann.Kind == Ref && len(ann.Args) > 0 && fcName(ann.Name) {
						return ann.Args[0], r.tok(j).off, nil
					}
					j = p.i
				}
				if r.isPunct(j, "=") {
					return r.exprProps(j+1, 0)
				}
			}
		}
		return nil, 0, r.errAt(r.tok(i).off, "can't find the component %s exports by default", name)
	}
	return r.exprProps(i, 0)
}

func fcName(n string) bool {
	switch strings.TrimPrefix(n, "React.") {
	case "FC", "FunctionComponent", "VFC", "ComponentType":
		return true
	}
	return false
}

// exprProps reads the props of a component expression at i: a function,
// an arrow, forwardRef(…)/memo(…) of one, or a name declared in the file.
func (r *reader) exprProps(i, depth int) (*Type, int, error) {
	if depth > 4 {
		return nil, 0, r.errAt(r.tok(i).off, "can't follow the component's definition")
	}
	switch {
	case r.isIdent(i, "async"):
		return r.exprProps(i+1, depth)
	case r.isIdent(i, "function"):
		j := i + 1
		if r.tok(j).kind == tIdent {
			j++
		}
		return r.funcProps(j, 0)
	case r.isPunct(i, "("):
		end := matching(r.toks, i)
		if end > 0 && r.isPunct(end+1, "=>") || end > 0 && r.isPunct(end+1, ":") {
			return r.funcProps(i, 0)
		}
		return r.exprProps(i+1, depth+1)
	case r.tok(i).kind == tIdent && r.isPunct(i+1, "=>"):
		return nil, 0, r.errAt(r.tok(i).off, "the component's props parameter %s has no type: annotate it (%s: Props)", r.tok(i).text, r.tok(i).text)
	case r.tok(i).kind == tIdent:
		name := r.tok(i).text
		j := i + 1
		for r.isPunct(j, ".") && r.tok(j+1).kind == tIdent {
			name += "." + r.tok(j+1).text
			j += 2
		}
		base := strings.TrimPrefix(name, "React.")
		switch base {
		case "forwardRef", "memo":
			var targs []*Type
			if r.isPunct(j, "<") {
				end := matching(r.toks, j)
				if end < 0 {
					return nil, 0, r.errAt(r.tok(i).off, "unbalanced %s<…>", name)
				}
				p := r.parserAt(j + 1)
				for !p.is(">") && p.err == nil {
					targs = append(targs, p.typ())
					if !p.eat(",") {
						break
					}
				}
				j = end + 1
			}
			if base == "forwardRef" && len(targs) == 2 {
				return targs[1], r.tok(i).off, nil
			}
			if base == "memo" && len(targs) == 1 {
				return targs[0], r.tok(i).off, nil
			}
			if r.isPunct(j, "(") {
				return r.exprProps(j+1, depth+1)
			}
		default:
			if !r.isPunct(j, "(") {
				return r.componentProps(name, i)
			}
		}
	}
	return nil, 0, r.errAt(r.tok(i).off, "can't read the component's props: export a function whose first parameter is typed (props: Props)")
}

// funcProps reads the type of the parameter at index n of the parameter
// list whose ( is at i (generic type parameters skipped).
func (r *reader) funcProps(i, n int) (*Type, int, error) {
	if r.isPunct(i, "<") {
		end := matching(r.toks, i)
		if end < 0 {
			return nil, 0, r.errAt(r.tok(i).off, "unbalanced type parameters")
		}
		i = end + 1
	}
	if !r.isPunct(i, "(") {
		return nil, 0, r.errAt(r.tok(i).off, "expected the component's parameters")
	}
	end := matching(r.toks, i)
	k := i + 1
	for idx := 0; k < end; idx++ {
		start := k
		var names []destructuredName
		switch {
		case r.isPunct(k, "{"):
			names, k = r.destructured(k)
			if k < 0 {
				return nil, 0, r.errAt(r.tok(start).off, "unbalanced destructuring")
			}
			k++
		case r.isPunct(k, "["):
			k = matching(r.toks, k) + 1
		case r.tok(k).kind == tIdent:
			k++
		default:
			return nil, 0, r.errAt(r.tok(k).off, "unexpected %q in the parameters", r.tok(k).text)
		}
		r.eatOpt(&k)
		var t *Type
		if r.isPunct(k, ":") {
			p := r.parserAt(k + 1)
			t = p.typ()
			if p.err != nil {
				return nil, 0, r.errAt(r.tok(k).off, "the props type: %v", p.err)
			}
			k = p.i
		}
		if r.isPunct(k, "=") {
			p := r.parserAt(k + 1)
			p.skipExpr()
			k = p.i
		}
		if idx == n {
			if t == nil {
				return nil, 0, r.errAt(r.tok(start).off, "the props parameter has no type: annotate it (props: Props)")
			}
			r.defaults = map[string]bool{}
			for _, d := range names {
				if d.hasDefault {
					r.defaults[d.name] = true
				}
			}
			return t, r.tok(start).off, nil
		}
		if !r.isPunct(k, ",") {
			break
		}
		k++
	}
	return nil, 0, nil
}

func (r *reader) eatOpt(k *int) {
	if r.isPunct(*k, "?") {
		*k++
	}
}

// svelte reads a Svelte component's props: Svelte 5's let { … }: Props =
// $props() (with $bindable defaults), or Svelte 4's export let.
func (r *reader) svelte(c *Component) error {
	for i := 0; i+1 < len(r.toks); i++ {
		if !r.isIdent(i, "$props") || !r.isPunct(i+1, "(") {
			continue
		}
		// let PATTERN [: T] = $props()
		if !r.isPunct(i-1, "=") {
			return r.errAt(r.tok(i).off, "$props() must be destructured: let { a, b }: Props = $props()")
		}
		k := i - 2
		var t *Type
		// Walk back to let/const/var.
		start := -1
		for j := k; j >= 0; j-- {
			if r.isIdent(j, "let") || r.isIdent(j, "const") || r.isIdent(j, "var") {
				start = j
				break
			}
		}
		if start < 0 {
			return r.errAt(r.tok(i).off, "$props() outside a let declaration")
		}
		j := start + 1
		var names []destructuredName
		if r.isPunct(j, "{") {
			var end int
			names, end = r.destructured(j)
			j = end + 1
		} else {
			j++
		}
		if r.isPunct(j, ":") {
			p := r.parserAt(j + 1)
			t = p.typ()
			if p.err != nil {
				return r.errAt(r.tok(j).off, "the props type: %v", p.err)
			}
		}
		if t == nil {
			return r.errAt(r.tok(i).off, "$props() has no type: let { … }: Props = $props()")
		}
		props, err := r.propsOf(t, r.tok(i).off)
		if err != nil {
			return err
		}
		by := map[string]destructuredName{}
		for _, n := range names {
			by[n.name] = n
		}
		for _, p := range props {
			if n, ok := by[p.Name]; ok {
				if n.hasDefault {
					p.Optional = true
				}
				if n.bindable {
					p.Model = true
				}
			}
		}
		c.Props = props
		return nil
	}
	// Svelte 4: export let name: T = default
	for i := 0; i+2 < len(r.toks); i++ {
		if !r.isIdent(i, "export") || !r.isIdent(i+1, "let") || r.tok(i+2).kind != tIdent {
			continue
		}
		name := r.tok(i + 2).text
		j := i + 3
		r.eatOpt(&j)
		var t *Type
		if r.isPunct(j, ":") {
			p := r.parserAt(j + 1)
			t = p.typ()
			if p.err != nil {
				return r.errAt(r.tok(j).off, "%s's type: %v", name, p.err)
			}
			j = p.i
		}
		if t == nil {
			return r.errAt(r.tok(i).off, "export let %s has no type: export let %s: T", name, name)
		}
		c.Props = append(c.Props, &Prop{Name: name, Type: t, Optional: r.isPunct(j, "="), Line: r.line(r.tok(i).off)})
	}
	return nil
}

// vanilla reads a vanilla island's props: the type of mount's second
// parameter, mount(el, props: Props, ctx).
func (r *reader) vanilla(c *Component) error {
	for i := 0; i+2 < len(r.toks); i++ {
		if !r.isIdent(i, "export") {
			continue
		}
		switch {
		case r.isIdent(i+1, "function") && r.isIdent(i+2, "mount"):
			return r.vanillaParams(c, i+3)
		case (r.isIdent(i+1, "const") || r.isIdent(i+1, "let")) && r.isIdent(i+2, "mount") && r.isPunct(i+3, "="):
			j := i + 4
			if r.isIdent(j, "function") {
				j++
				if r.tok(j).kind == tIdent {
					j++
				}
			}
			if r.isIdent(j, "async") {
				j++
			}
			return r.vanillaParams(c, j)
		}
	}
	return r.errAt(0, "no export function mount(el, props, ctx): a vanilla island exports mount")
}

func (r *reader) vanillaParams(c *Component, i int) error {
	t, off, err := r.funcProps(i, 1)
	if err != nil {
		return err
	}
	if t == nil {
		return nil // mount(el): no props
	}
	props, err := r.propsOf(t, off)
	if err != nil {
		return err
	}
	c.Props = props
	r.applyDefaults(c.Props)
	return nil
}
