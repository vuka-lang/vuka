package live

import (
	"reflect"
	"strings"

	"github.com/vuka-lang/vuka"
)

// treeUpdates is what a v2 client needs after a render: each instance's
// whole tree when it holds none, else the change, in render order.
func (s *Session) treeUpdates() []TreeUpdate {
	var ups []TreeUpdate
	for _, in := range s.order() {
		if in.tree == nil || !in.rendered && in.shadow != nil {
			continue
		}
		e := enc{c: &s.c}
		s.c.begin()
		u := TreeUpdate{ID: in.id}
		switch {
		case in.shadow == nil || in.shadow.key != in.tree.Key:
			u.Full = true
			e.frame(in.tree)
		case !e.frameChange(in.shadow, in.tree):
			continue
		case len(e.b) > fallbackAt && !hasRefs(in.tree):
			var b strings.Builder
			in.tree.HTML(&b, nil)
			if b.Len() < len(e.b) {
				s.c.rollback()
				in.shadow = nil
				ups = append(ups, TreeUpdate{ID: in.id, HTML: b.String()})
				continue
			}
		}
		u.Tree = e.b
		in.shadow = shadow(in.tree)
		ups = append(ups, u)
	}
	return ups
}

// treeHTML writes an instance's HTML from its tree, the instances it renders
// in place.
func (in *instance) treeHTML(b *strings.Builder) {
	if in.tree == nil {
		return
	}
	in.tree.HTML(b, func(b *strings.Builder, id string) {
		if c := in.s.byID[id]; c != nil && c != in {
			c.treeHTML(b)
		}
	})
}

// splitMarkers turns markup holding instances' markers (an opaque node
// rendering a stateful component) into frames of the markup around
// references to them.
func splitMarkers(t *vuka.Tree) *vuka.Tree {
	for i, d := range t.Dyn {
		switch d := d.(type) {
		case string:
			if strings.Contains(d, "\x00vk:") {
				t.Dyn[i] = markerTree(d)
			}
		case *vuka.Tree:
			splitMarkers(d)
		case *vuka.TreeList:
			for _, it := range d.Items {
				splitMarkers(it)
			}
		}
	}
	return t
}

func markerTree(s string) *vuka.Tree {
	var parts []any
	for {
		i := strings.Index(s, "\x00vk:")
		j := -1
		if i >= 0 {
			j = strings.IndexByte(s[i+4:], 0)
		}
		if j < 0 {
			return vuka.OpaqueTree(append(parts, s)...)
		}
		parts = append(parts, s[:i], vuka.TreeRef(s[i+4:i+4+j]))
		s = s[i+5+j:]
	}
}

// sameProps reports whether a tag sets the props an instance has: every
// exported field equal, compared by value. Pointers, slices, maps, funcs,
// channels and interfaces (children included) count as the same only when
// both are nil, since what they point to may have changed in place.
func sameProps(to, from vuka.Stateful) bool {
	dst, src := reflect.ValueOf(to).Elem(), reflect.ValueOf(from).Elem()
	live := reflect.TypeFor[vuka.Live]()
	for i := 0; i < dst.NumField(); i++ {
		if f := dst.Type().Field(i); f.IsExported() && f.Type != live && !sameValue(dst.Field(i), src.Field(i)) {
			return false
		}
	}
	return true
}

func sameValue(a, b reflect.Value) bool {
	switch a.Kind() {
	case reflect.Bool:
		return a.Bool() == b.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return a.Int() == b.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return a.Uint() == b.Uint()
	case reflect.Float32, reflect.Float64:
		return a.Float() == b.Float()
	case reflect.Complex64, reflect.Complex128:
		return a.Complex() == b.Complex()
	case reflect.String:
		return a.String() == b.String()
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			if !sameValue(a.Field(i), b.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Array:
		for i := 0; i < a.Len(); i++ {
			if !sameValue(a.Index(i), b.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Func, reflect.Chan, reflect.Interface, reflect.UnsafePointer:
		return a.IsNil() && b.IsNil()
	}
	return false
}
