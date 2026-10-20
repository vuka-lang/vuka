package check

import (
	"fmt"
	"go/types"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"
)

// Live checks the pages registered with web: a live page's constructor and
// struct (E0329, E0330), and a page backed by its island as ui.CheckIsland
// does at boot — bind: (E0322), defer: (E0323), a deferred group without a
// loader (E0324) or with a wrong one (E0325), two handles of one name
// (E0326), Changed (E0327), validate: (E0328) and Mount (E0333) — and
// the page's LiveSchema: its fields (E0335) and hooks (E0336), and the
// hooks its constructor declares on its ui.Live (E0336, W0304, W0305).
var Live = &analysis.Analyzer{
	Name: "live",
	Doc:  "check vuka live pages and pages backed by an island as the app's boot does",
	URL:  docsURL,
	Run:  runLive,
}

func runLive(pass *analysis.Pass) (any, error) {
	l := &liveCheck{pass: pass, done: map[string]bool{}}
	l.ctor = l.ctorHooks()
	for _, r := range registrations(pass) {
		if r.verb == "Live" {
			l.livePage(r)
			continue
		}
		if r.method != "" && r.island(pass.TypesInfo) {
			if n := r.pageType(pass.TypesInfo); n != nil {
				l.page(n, r, true, false)
			}
		}
	}
	return nil, nil
}

type liveCheck struct {
	pass *analysis.Pass
	r    registration
	done map[string]bool // reported, by place and message
	ctor map[*types.Named]*ctorHooks
}

func (l *liveCheck) report(at analysis.Range, code, help, format string, args ...any) {
	k := fmt.Sprint(at.Pos(), code, fmt.Sprintf(format, args...))
	if !l.done[k] {
		l.done[k] = true
		report(l.pass, at, code, help, format, args...)
	}
}

func (l *liveCheck) livePage(r registration) {
	t := l.pass.TypesInfo.TypeOf(r.fn)
	if t == nil || types.IsInterface(t) {
		return
	}
	if sig, ok := t.Underlying().(*types.Signature); ok {
		if _, why := ctor(sig, qualifier(l.pass)); why != "" {
			report(l.pass, r.fn, "E0329", "pass the page's constructor: func(deps…) *Page", "Live(%q, …): %s", r.path, why)
			return
		}
	}
	n := r.pageType(l.pass.TypesInfo)
	if n == nil {
		report(l.pass, r.fn, "E0329", "pass NewPage, a func(deps…) *Page", "Live(%q, …) takes the page's constructor, not a %s", r.path, typeString(l.pass, t))
		return
	}
	l.page(n, r, r.island(l.pass.TypesInfo), true)
}

// page checks the struct n a route serves.
func (l *liveCheck) page(n *types.Named, r registration, island, live bool) {
	l.r = r
	pass := l.pass
	st := n.Underlying().(*types.Struct)
	name := n.Obj().Name()
	ptr := types.NewPointer(n)
	stateful := iface(pass, uiPath, "Stateful")
	var embed *types.Var
	for i := range st.NumFields() {
		if f := st.Field(i); f.Embedded() && named(f.Type(), uiPath, "Live") {
			embed = f
		}
	}
	if embed != nil {
		if _, ok := embed.Type().(*types.Pointer); ok {
			l.report(at(pass, embed, r.at), "E0329", "embed ui.Live by value", "%s embeds *ui.Live", name)
			return
		}
	}
	if live && stateful != nil && !types.Implements(ptr, stateful) {
		if island && embed == nil {
			l.report(at(pass, n.Obj(), r.at), "E0330", "embed ui.Live, or route it with Get (a stateless page)", "%s is a page backed by its island under Live, which is live, and it doesn't embed ui.Live", name)
		} else {
			l.report(at(pass, n.Obj(), r.at), "E0329", "embed ui.Live in a struct with a Render() ui.Node method", "%s is not a stateful component", name)
		}
		return
	}
	ms := types.NewMethodSet(ptr)
	schema := l.ctor[n].merge(l.schema(n))
	if m := ms.Lookup(nil, "Mount"); m != nil {
		sig := m.Type().(*types.Signature)
		p, res := sig.Params(), sig.Results()
		okIn := p.Len() == 0 || p.Len() == 1 && isContext(p.At(0).Type())
		okOut := res.Len() == 0 || res.Len() == 1 && types.Identical(res.At(0).Type(), errorType)
		if !okIn || !okOut {
			code := "E0329"
			if island {
				code = "E0333"
			}
			l.report(at(pass, m.Obj(), r.at), code, "declare Mount() or Mount(ctx context.Context), returning nothing or an error", "%s's Mount is %s", name, typeString(pass, sig))
		}
	}
	if island {
		l.island(n, st, ms, schema)
	}
}

// reserved are live's own members on the browser's live object.
var reserved = map[string]bool{"pending": true, "error": true, "subscribe": true}

// lifecycle are a live component's methods its session calls, never the
// component.
var lifecycle = map[string]bool{"Render": true, "Mount": true, "Update": true, "Info": true, "Changed": true}

// island checks a page backed by its island as ui.CheckIsland does.
func (l *liveCheck) island(n *types.Named, st *types.Struct, ms *types.MethodSet, schema *liveSchema) {
	if schema == nil {
		schema = &liveSchema{}
	}
	pass := l.pass
	name := n.Obj().Name()
	node := iface(pass, uiPath, "Node")
	jsChain := iface(pass, uiPath, "JSChain")
	anyState := iface(pass, uiPath, "AnyState")
	handles := map[string]string{}
	type group struct {
		name  string
		field *types.Var
		at    analysis.Range
	}
	var groups []group
	seenGroup := map[string]bool{}
	for i := range st.NumFields() {
		f := st.Field(i)
		tag := reflect.StructTag(st.Tag(i))
		fat := at(pass, f, l.r.at)
		so := schema.fields[f]
		bind, hasBind := tag.Lookup("bind")
		hasBind = hasBind && bind != "-"
		what, sat := "bind:", fat
		if so.at != nil {
			sat = so.at // what the LiveSchema says is reported there
		}
		if so.bind {
			what = "ui.Bind"
		}
		wire, _, _ := strings.Cut(tag.Get("json"), ",")
		ft := f.Type()
		_, isFunc := ft.Underlying().(*types.Signature)
		prop := f.Exported() && !(f.Embedded() && is(ft, uiPath, "Live")) && wire != "-" &&
			!(node != nil && types.IsInterface(ft) && implements(ft, node)) && !isFunc &&
			!implements(ft, jsChain) && !implements(ft, anyState)
		if !prop {
			if hasBind || so.bind {
				l.report(sat, "E0322", "drop "+what+", or make "+f.Name()+" an exported field of a plain value",
					"%s.%s: %s is for a prop the component sets (an exported field of a JSON value)", name, f.Name(), what)
			}
			continue
		}
		if wire == "" {
			wire = f.Name()
		}
		handle := lowerFirst(wire)
		if v, ok := tag.Lookup("validate"); ok {
			if msg, help := validateWhy(v); msg != "" {
				l.report(fat, "E0328", help, "%s.%s: validate:%q: %s", name, f.Name(), v, msg)
			}
		}
		l.validateType(ft, fat, map[types.Type]bool{})
		if other, ok := handles[handle]; ok {
			l.report(fat, "E0326", "give one of them another json name", "%s: fields %s and %s are both live.%s", name, other, f.Name(), handle)
		} else {
			handles[handle] = f.Name()
		}
		d, hasDefer := tag.Lookup("defer")
		form := fmt.Sprintf("defer:%q", d)
		if so.deferred {
			d, hasDefer, form = so.group, true, fmt.Sprintf("ui.Defer(%q)", so.group)
		}
		if hasDefer && d != "-" {
			if d == "" {
				d = f.Name()
			}
			q, _, _ := strings.Cut(tag.Get("url"), ",")
			switch {
			case !isIdent(d):
				l.report(sat, "E0323", `name the group: ui.Defer("stats"), loaded by l.Loads("stats", …)`, "%s.%s: %s: a group is a name", name, f.Name(), form)
			case hasBind || so.bind:
				l.report(sat, "E0323", "drop Bind or Defer", "%s.%s: a deferred field is the server's to load", name, f.Name())
			case so.url != "" || q != "" && q != "-":
				l.report(sat, "E0323", "drop URL or Defer", "%s.%s: a deferred field is the server's to load", name, f.Name())
			default:
				g := strings.ToUpper(d[:1]) + d[1:]
				if !seenGroup[g] {
					seenGroup[g] = true
					groups = append(groups, group{g, f, sat})
				}
			}
		}
		switch {
		case so.bind && reserved[handle]:
			l.report(sat, "E0322", "give the field another json name to bind it", "%s.%s: live.%s is live's own", name, f.Name(), handle)
		case hasBind && !so.bind:
			l.bind(name, f, fat, bind, handle, wire)
		}
	}
	loaders := map[string]bool{}
	for _, g := range groups {
		if schema.loads[g.name] {
			continue
		}
		loader := "Load" + g.name
		loaders[loader] = true
		m := ms.Lookup(nil, loader)
		if m == nil {
			l.report(g.at, "E0324", fmt.Sprintf("give the group a loader in the page's LiveSchema:\n\t\tl.Loads(%q, %s.load%s)", lowerFirst(g.name), strings.ToLower(name[:1]), g.name),
				"%s.%s is deferred, and the group %s has no loader", name, g.field.Name(), g.name)
			continue
		}
		sig := m.Type().(*types.Signature)
		p, res := sig.Params(), sig.Results()
		if !(p.Len() == 0 || p.Len() == 1 && isContext(p.At(0).Type())) || !(res.Len() == 0 || res.Len() == 1 && types.Identical(res.At(0).Type(), errorType)) {
			l.report(at(pass, m.Obj(), l.r.at), "E0325", fmt.Sprintf("declare it %s() or %s(ctx context.Context), returning nothing or an error", loader, loader),
				"%s.%s is the loader of the deferred group %s, and it is %s", name, loader, g.name, typeString(pass, sig))
		}
	}
	own := map[string]bool{}
	if live := lookup(pass.Pkg, uiPath, "Live"); live != nil {
		lms := types.NewMethodSet(types.NewPointer(live.Type()))
		for i := range lms.Len() {
			own[lms.At(i).Obj().Name()] = true
		}
	}
	for i := range ms.Len() {
		m := ms.At(i).Obj().(*types.Func)
		mn := m.Name()
		if !m.Exported() || lifecycle[mn] || own[mn] || isUpdate(m) || loaders[mn] || schema.hooks[mn] || mn == "LiveSchema" {
			continue
		}
		sig := m.Type().(*types.Signature)
		if res := sig.Results(); res.Len() > 1 || res.Len() == 1 && !types.Identical(res.At(0).Type(), errorType) || sig.Variadic() {
			continue
		}
		for j := range sig.Params().Len() {
			l.validateType(sig.Params().At(j).Type(), at(pass, m, l.r.at), map[types.Type]bool{})
		}
		if f, ok := handles[lowerFirst(mn)]; ok {
			l.report(at(pass, m, l.r.at), "E0326", "rename one, or give the field another json name", "%s: the method %s and the field %s are both live.%s", name, mn, f, lowerFirst(mn))
		}
	}
	if m := ms.Lookup(nil, "Changed"); m != nil && !schema.onChange && !changedOK(m.Type().(*types.Signature)) {
		l.report(at(pass, m.Obj(), l.r.at), "E0327", "declare Changed(), Changed(ctx context.Context) or Changed(ctx context.Context, changed ui.Changed), returning nothing or an error",
			"%s.Changed is %s", name, typeString(pass, m.Type()))
	}
}

func (l *liveCheck) bind(name string, f *types.Var, fat analysis.Range, bind, handle, wire string) {
	n, opts, _ := strings.Cut(bind, ",")
	switch {
	case n != "" && n != handle && n != wire:
		l.report(fat, "E0322", fmt.Sprintf(`write bind:%q, or leave the name out: bind:""`, handle), "%s.%s: bind:%q names the field live.%s", name, f.Name(), n, handle)
	case reserved[handle]:
		l.report(fat, "E0322", "give the field another json name to bind it", "%s.%s: live.%s is live's own", name, f.Name(), handle)
	}
	for _, o := range strings.Split(opts, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(o), "=")
		switch k {
		case "":
		case "debounce":
			if !validDebounce(v) {
				l.report(fat, "E0322", "debounce is milliseconds (debounce=500) or a duration (debounce=1s)", "%s.%s: bind:%q: bad debounce %q", name, f.Name(), bind, v)
			}
		default:
			l.report(fat, "E0322", "did you mean debounce=500?", "%s.%s: bind:%q: unknown option %q", name, f.Name(), bind, k)
		}
	}
}

// validateType checks the validate: tags of the structs t holds, as
// ui.CheckValidate does.
func (l *liveCheck) validateType(t types.Type, fallback analysis.Range, seen map[types.Type]bool) {
	for {
		switch x := t.Underlying().(type) {
		case *types.Pointer:
			t = x.Elem()
			continue
		case *types.Slice:
			t = x.Elem()
			continue
		case *types.Array:
			t = x.Elem()
			continue
		case *types.Map:
			t = x.Elem()
			continue
		case *types.Struct:
			if seen[t] {
				return
			}
			seen[t] = true
			for i := range x.NumFields() {
				f := x.Field(i)
				if v, ok := reflect.StructTag(x.Tag(i)).Lookup("validate"); ok {
					if msg, help := validateWhy(v); msg != "" {
						l.report(at(l.pass, f, fallback), "E0328", help, "%s: validate:%q: %s", f.Name(), v, msg)
					}
				}
				l.validateType(f.Type(), fallback, seen)
			}
		}
		return
	}
}

// validateWhy is what is wrong with a validate: tag, and the fix.
func validateWhy(tag string) (string, string) {
	if tag == "" || tag == "-" {
		return "", ""
	}
	for _, part := range strings.Split(tag, ",") {
		name, param, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch name {
		case "":
		case "required", "omitempty", "email":
			if param != "" {
				return name + " takes no value", "write " + name + " alone"
			}
		case "min", "max", "len":
			if _, err := strconv.ParseFloat(param, 64); err != nil {
				return name + " takes a number", "write " + name + "=3"
			}
		case "oneof":
			if len(strings.FieldsFunc(param, func(c rune) bool { return c == ' ' || c == '|' })) == 0 {
				return "oneof takes values", "write oneof=a b c"
			}
		default:
			return fmt.Sprintf("unknown rule %q", name), "the rules are required, omitempty, min, max, len, oneof and email"
		}
	}
	return "", ""
}

func changedOK(sig *types.Signature) bool {
	var args string
	for i := range sig.Params().Len() {
		t := sig.Params().At(i).Type()
		switch {
		case isContext(t) && args == "":
			args += "c"
		case is(t, uiPath, "Changed") && !strings.Contains(args, "n"):
			args += "n"
		default:
			return false
		}
	}
	res := sig.Results()
	return res.Len() == 0 || res.Len() == 1 && types.Identical(res.At(0).Type(), errorType)
}

func validDebounce(v string) bool {
	if n, err := strconv.Atoi(v); err == nil {
		return n >= 0
	}
	d, err := time.ParseDuration(v)
	return err == nil && d >= 0
}

func isIdent(s string) bool {
	for i, r := range s {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return s != ""
}

// isUpdate reports whether m is an overload of a live component's Update,
// which its session calls with a message: UpdateT taking a T last, T
// written as part of a name as vuka names overloads (ui.IsUpdate), or
// Update__T as older vuka commands named them.
func isUpdate(m *types.Func) bool {
	mn := m.Name()
	if strings.HasPrefix(mn, "Update__") {
		return true
	}
	p := m.Type().(*types.Signature).Params()
	return strings.HasPrefix(mn, "Update") && p.Len() > 0 && mn == "Update"+typeName(p.At(p.Len()-1).Type())
}

// typeName is t written as part of a Go name: int Int, *Order Order,
// []Order Orders, map[string]int MapStringInt, any Any.
func typeName(t types.Type) string {
	switch t := types.Unalias(t).(type) {
	case *types.Named:
		name := t.Obj().Name()
		for a := range t.TypeArgs().Types() {
			name += typeName(a)
		}
		return upperFirst(name)
	case *types.Basic:
		return upperFirst(t.Name())
	case *types.Pointer:
		return typeName(t.Elem())
	case *types.Slice:
		return plural(typeName(t.Elem()))
	case *types.Array:
		return plural(typeName(t.Elem()))
	case *types.Map:
		return "Map" + typeName(t.Key()) + typeName(t.Elem())
	case *types.Chan:
		return "Chan" + typeName(t.Elem())
	case *types.Signature:
		return "Func"
	case *types.Interface:
		if t.Empty() {
			return "Any"
		}
		return "Interface"
	case *types.Struct:
		return "Struct"
	}
	return upperFirst(t.String())
}

func upperFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}

func plural(s string) string {
	switch {
	case strings.HasSuffix(s, "s"), strings.HasSuffix(s, "x"), strings.HasSuffix(s, "z"),
		strings.HasSuffix(s, "ch"), strings.HasSuffix(s, "sh"):
		return s + "es"
	case len(s) > 1 && s[len(s)-1] == 'y' && !strings.ContainsRune("aeiouAEIOU", rune(s[len(s)-2])):
		return s[:len(s)-1] + "ies"
	}
	return s + "s"
}
