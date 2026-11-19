package transpile

import (
	"go/ast"
	"go/types"
	"sort"
	"strconv"
	"strings"
	"time"
)

// On a live component a <ui.Suspense> waits for deferred groups: those of
// the fields for={…} names, else of the @ui.Defer fields its body reads.
// Its <slot:fallback> shows while any of them loads, its <slot:error> once a
// loader failed, else its body:
//
//	<ui.Suspense for={tiles, tabs}>
//	  <slot:fallback><ui.Skeleton lines={3}/></slot:fallback>
//	  …
//	</ui.Suspense>
//
// is one Await for the groups, the first its Key and the rest its And:
//
//	ui.Await{Of: p, Key: "stats", And: []string{"board"}, Pending: …, Error: …, Children: func() ui.Node { … }}
//
// Its wait, delay and minShow attributes (duration strings) are the Await's
// Wait, Delay and MinShow.

// liveBoundary reports whether f's markup is a live component's, where a
// <ui.Suspense> waits for deferred groups.
func (e *engine) liveBoundary(f *fileState) bool {
	h := f.html
	return h != nil && h.recvType != "" && e.liveSchema(h.recvType) != nil
}

// deferredFields are the live type typeName's @ui.Defer fields, each with
// the group it loads in as written: @ui.Defer("stats") is stats, a bare
// @ui.Defer the field's name.
func (e *engine) deferredFields(typeName string) map[string]string {
	si := e.liveSchema(typeName)
	if si == nil {
		return nil
	}
	out := map[string]string{}
	for _, a := range si.file.attrs {
		if !a.Field || a.structOf != typeName || lastName(a.Name) != "Defer" || len(a.fieldNames) == 0 {
			continue
		}
		for _, name := range a.fieldNames {
			g := name
			if s, err := strconv.Unquote(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(a.Args, "("), ")"))); err == nil && s != "" {
				g = s
			}
			out[name] = g
		}
	}
	return out
}

// suspenseLive decides the groups a live component's boundary waits for:
// for={…}'s fields (a name not a deferred field is E0352), else the
// deferred fields its body reads; none is W0352.
func (e *engine) suspenseLive(f *fileState, el *jsxElem) {
	h := f.html
	fields := e.deferredFields(h.recvType)
	var names []string
	for n := range fields {
		names = append(names, n)
	}
	sort.Strings(names)
	var groups []string
	add := func(g string) {
		for _, have := range groups {
			if groupName(have) == groupName(g) {
				return
			}
		}
		groups = append(groups, g)
	}
	if a := attrNamed(el, "for"); a != nil && a.kind == 'e' {
		text := string(f.src[a.expr.start:a.expr.end])
		for _, raw := range strings.Split(text, ",") {
			name := strings.TrimSpace(raw)
			g, ok := fields[name]
			if !ok {
				err := e.errs.add("E0352", f.at(a.expr.start), "<%s for={%s}>: %s is not a deferred field of %s", el.tag, text, name, h.recvType)
				if near := closestName(name, names); near != "" {
					err.Suggest = near
				}
				err.help("on a live component, for={…} names fields marked @%sDefer(\"group\")", f.target.q)
				continue
			}
			add(g)
		}
	} else {
		for _, name := range e.boundaryReads(f, el, fields) {
			add(fields[name])
		}
		if len(groups) == 0 {
			f.susWarnings.add("W0352", f.at(el.start), "<%s> on live %s waits for nothing: its body reads no deferred field, so it renders at once", el.tag, h.recvType).
				help("read a field marked @%sDefer(\"group\") inside it, or name the fields to wait for with for={…}", f.target.q)
		}
	}
	if len(groups) == 0 {
		return
	}
	if f.susGroups == nil {
		f.susGroups = map[*jsxElem][]string{}
	}
	f.susGroups[el] = groups
}

// boundaryReads are the deferred fields el's body reads, in the order it
// first reads them.
func (e *engine) boundaryReads(f *fileState, el *jsxElem, fields map[string]string) []string {
	var fieldVars map[string]*types.Var
	if obj, _ := e.pkg.Scope().Lookup(f.html.recvType).(*types.TypeName); obj != nil {
		if st, ok := obj.Type().Underlying().(*types.Struct); ok {
			fieldVars = map[string]*types.Var{}
			for i := 0; i < st.NumFields(); i++ {
				fieldVars[st.Field(i).Name()] = st.Field(i)
			}
		}
	}
	type read struct {
		name string
		at   int
	}
	var reads []read
	seen := map[string]bool{}
	for _, r := range e.markupRoots(f) {
		ast.Inspect(r, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || seen[id.Name] || e.info.Defs[id] != nil {
				return true
			}
			if _, deferred := fields[id.Name]; !deferred {
				return true
			}
			if o := e.info.Uses[id]; o != nil && o != types.Object(fieldVars[id.Name]) {
				return true
			}
			at := f.orig(id.Pos())
			if at <= el.tagEnd || at >= el.end {
				return true
			}
			seen[id.Name] = true
			reads = append(reads, read{id.Name, at})
			return true
		})
	}
	sort.Slice(reads, func(i, j int) bool { return reads[i].at < reads[j].at })
	out := make([]string, len(reads))
	for i, r := range reads {
		out[i] = r.name
	}
	return out
}

// suspenseAwait writes a live component's boundary: one Await for the
// groups, the first its Key and the rest its And, with the boundary's
// timings.
func (w *jsxWriter) suspenseAwait(el *jsxElem, groups []string) {
	fallback, failed, body := suspenseSlots(el)
	of := "nil"
	if h := w.f.html; h != nil && h.recv != "" {
		of = h.recv
	}
	w.gen(w.q + "Await{Of: " + of + ", Key: " + strconv.Quote(groups[0]))
	if len(groups) > 1 {
		w.gen(", And: []string{")
		for i, g := range groups[1:] {
			if i > 0 {
				w.gen(", ")
			}
			w.gen(strconv.Quote(g))
		}
		w.gen("}")
	}
	for _, name := range suspenseTimings {
		d, ok := timingOf(el, name)
		switch {
		case !ok:
		case d == 0 && name == "delay":
			w.gen(", Delay: " + w.q + "Instant")
		case d != 0:
			w.gen(", " + strings.ToUpper(name[:1]) + name[1:] + ": " + strconv.FormatInt(int64(d), 10))
		}
	}
	if fallback != nil && hasContent(fallback.kids) {
		w.gen(", Pending: ")
		w.piece(fallback.kids)
	}
	if failed != nil && hasContent(failed.kids) {
		w.gen(", Error: ")
		w.slotFunc(nil, []string{"error"}, func() { w.piece(failed.kids) })
	}
	if hasContent(body) {
		w.gen(", Children: ")
		w.slotFunc(nil, nil, func() { w.piece(body) })
	}
	w.gen("}")
}

// suspenseTimings are the attributes of a live boundary that keep a fast
// load from flashing its fallback, in the order the Await's fields are
// written.
var suspenseTimings = []string{"wait", "delay", "minShow"}

// timingOf is the duration a boundary's timing attribute spells; false when
// it has none or it is not a valid duration string.
func timingOf(el *jsxElem, name string) (time.Duration, bool) {
	a := attrNamed(el, name)
	if a == nil || a.kind != 's' {
		return 0, false
	}
	d, err := time.ParseDuration(a.val)
	return d, err == nil && d >= 0
}
