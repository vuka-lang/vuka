package transpile

// renderRegistry records, at init, the typed attributes of each function,
// method and type of f carrying any (a bundle's count), for the runtime's
// lookups (vuka.MethodAttrs, vuka.TypeAttrs, vuka.FuncOf): one init for the
// file, declarations in source order. Generic declarations aren't recorded:
// there is no one function or type to name.
func (e *engine) renderRegistry(f *fileState) {
	w := &f.deco
	started := false
	start := func(off int) string {
		rt := f.runtime()
		if !started {
			started = true
			w.gen("\nfunc init() {\n", off)
		}
		w.gen("\t", off)
		return rt
	}
	typeDecos := map[int]*typeDeco{}
	for _, d := range f.decos {
		if t, ok := d.(*typeDeco); ok {
			typeDecos[t.nameOff] = t
		}
	}
	type item struct {
		off   int
		attrs []*Attr
		uses  []*decoUse
		fn    string // a function's or method expression's Go text; "" for a type
		typ   string
	}
	var items []item
	for _, d := range f.decos {
		switch d := d.(type) {
		case *funcDeco:
			if d.init || d.generic || len(d.attrs) == 0 && !hasBundle(d.uses) {
				continue
			}
			fn := d.name
			if d.method {
				fn = "(" + d.rtype + ")." + d.name
			}
			items = append(items, item{off: d.nameOff, attrs: d.attrs, uses: d.uses, fn: fn})
		case *typeDeco:
			if len(d.attrs) == 0 && !hasBundle(d.uses) {
				continue
			}
			items = append(items, item{off: d.nameOff, attrs: d.attrs, uses: d.uses, typ: d.name})
		}
	}
	seen := map[int]bool{}
	for _, a := range f.attrs {
		if a.kind != attrTyped || a.Field || a.Param {
			continue
		}
		for _, r := range a.regs {
			if seen[r.off] {
				continue
			}
			seen[r.off] = true
			if r.typ {
				if typeDecos[r.off] == nil {
					items = append(items, item{off: r.off, attrs: typedAttrs(f, a.decl), typ: r.name})
				}
				continue
			}
			if _, ok := f.decorated[a.declOff]; ok || r.recv == "" && r.name == "init" || r.name == "_" {
				continue
			}
			name, key := r.name, r.name
			if r.recv != "" {
				key = r.recvBase + "." + r.name
			}
			if set := e.sets[key]; set != nil {
				for _, o := range set.list {
					if o.nameOff == r.off {
						name = o.mangled
					}
				}
			}
			fn := name
			if r.recv != "" {
				fn = "(" + r.recv + ")." + name
			}
			items = append(items, item{off: r.off, attrs: typedAttrsAt(f, a.declOff), fn: fn})
		}
	}
	sortItems(items, func(i item) int { return i.off })
	for _, it := range items {
		rt := start(it.off)
		if it.typ != "" {
			w.gen(rt+".RegisterType["+it.typ+"](", it.off)
		} else {
			w.gen(rt+".RegisterFunc("+it.fn+", ", it.off)
		}
		e.writeAttrsWith(f, it.attrs, it.uses, it.off)
		w.gen("...)\n", it.off)
	}
	if started {
		w.gen("}\n", items[len(items)-1].off)
	}
}

func hasBundle(uses []*decoUse) bool {
	for _, u := range uses {
		if u.kind == decoBundle {
			return true
		}
	}
	return false
}

// sortItems sorts s by key, stably.
func sortItems[T any](s []T, key func(T) int) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && key(s[j]) < key(s[j-1]); j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
