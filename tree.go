package vuka

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// Tree is a frame of a live render tree: the markup of a piece of JSX as
// statics, the strings its source fixes, around dynamics, what its
// expressions rendered. Its HTML is Statics[0] + Dyn[0] + Statics[1] + … +
// Statics[len(Dyn)]. A dynamic is a string of HTML, a nested *Tree, a
// *TreeList (a block's items) or a TreeRef (a stateful component instance,
// rendered by the session as a tree of its own).
//
// Key identifies the statics: the frame's fingerprint in its render context.
// Two trees with the same Key have the same Statics, which are shared and
// must not be modified.
type Tree struct {
	Key     uint64
	Statics []string
	Dyn     []any
}

// TreeList is a {for}, {if} or {match} block's items, a frame per iteration
// or branch taken. Keys are the items' keys, from the key attribute of each
// item's first element or component tag; nil unless every item has one.
type TreeList struct {
	Items []*Tree
	Keys  []string
}

// TreeRef is a stateful component instance in a render tree: its id.
type TreeRef string

// TreeHost is a LiveHost that can render a stateful component's tag as a
// reference to its instance. Package live implements it.
type TreeHost interface {
	LiveHost
	ComponentRef(ctx context.Context, n *ComponentNode) (id string, err error)
}

// BuildTree renders n as a render tree. A Frame becomes a Tree split as its
// shape says; anything else is a frame of one dynamic. ctx should carry a
// live host; with verify, statics already known for a Key are rendered again
// and compared, and a difference is an error.
func BuildTree(ctx context.Context, n Node, verify bool) (*Tree, error) {
	b := &treeBuilder{verify: verify}
	b.h = HTMLRenderer{ctx: ctx}
	if f, ok := n.(*Frame); ok && f != nil {
		return b.frame(ctx, f)
	}
	v, err := b.value(ctx, n)
	if err != nil {
		return nil, err
	}
	return OpaqueTree(v), nil
}

// OpaqueTree is a frame with no statics of its own around dyns.
func OpaqueTree(dyns ...any) *Tree {
	return &Tree{Key: opaqueKey(len(dyns)), Statics: emptyStatics(len(dyns) + 1), Dyn: dyns}
}

// HTML writes t's HTML into b, each TreeRef as ref writes it.
func (t *Tree) HTML(b *strings.Builder, ref func(b *strings.Builder, id string)) {
	b.WriteString(t.Statics[0])
	for i, d := range t.Dyn {
		switch d := d.(type) {
		case string:
			b.WriteString(d)
		case *Tree:
			d.HTML(b, ref)
		case *TreeList:
			for _, it := range d.Items {
				it.HTML(b, ref)
			}
		case TreeRef:
			if ref != nil {
				ref(b, string(d))
			}
		}
		b.WriteString(t.Statics[i+1])
	}
}

var (
	staticsCache sync.Map // Key → []string
	emptyCache   sync.Map // n → n empty strings
)

func emptyStatics(n int) []string {
	if v, ok := emptyCache.Load(n); ok {
		return v.([]string)
	}
	s := make([]string, n)
	emptyCache.Store(n, s)
	return s
}

func mix(x uint64) uint64 { // splitmix64's finalizer
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	return x ^ x>>31
}

func treeKey(fp uint64, mode uint8, extra int) uint64 {
	return mix(fp ^ uint64(mode)<<56 ^ uint64(extra)<<48)
}

func opaqueKey(n int) uint64 { return mix(0x6f70617175650000 ^ uint64(n)) }

// treeBuilder renders frames: one HTMLRenderer whose writer moves between a
// frame's current static piece and its dynamics, so the context (script,
// style) carries across them as it does in HTML.
type treeBuilder struct {
	h      HTMLRenderer
	verify bool
}

// frameBuild is a frame being built.
type frameBuild struct {
	collect bool // the statics are wanted: unknown, or verified
	statics []string
	cur     strings.Builder
	dyn     []any
}

type discard struct{}

func (discard) WriteString(s string) (int, error) { return len(s), nil }

// toStatic directs output to the current static piece.
func (b *treeBuilder) toStatic(fb *frameBuild) {
	if fb.collect {
		b.h.w = &fb.cur
	} else {
		b.h.w = discard{}
	}
}

// endStatic closes the current static piece.
func (fb *frameBuild) endStatic() {
	if fb.collect {
		fb.statics = append(fb.statics, fb.cur.String())
		fb.cur.Reset()
	}
}

func (b *treeBuilder) frame(ctx context.Context, f *Frame) (*Tree, error) {
	key := treeKey(f.FP, b.h.mode, f.extra)
	known, ok := staticsCache.Load(key)
	sh := shape{s: f.Shape}
	if !sh.fits(f.Kids, f.extra) || !sh.done() {
		s, err := b.html(ctx, f)
		return OpaqueTree(s), err
	}
	fb := &frameBuild{collect: !ok || b.verify}
	sh.i = 0
	saved := b.h.w
	defer func() { b.h.w = saved }()
	if err := b.items(ctx, fb, &sh, f.Kids, f.extra); err != nil {
		return nil, err
	}
	fb.endStatic()
	t := &Tree{Key: key, Dyn: fb.dyn}
	switch {
	case !ok:
		staticsCache.Store(key, fb.statics)
		t.Statics = fb.statics
	default:
		t.Statics = known.([]string)
		if b.verify && !slices.Equal(t.Statics, fb.statics) {
			return nil, fmt.Errorf("vuka: frame %#x rendered statics %q, but %q before: two pieces of JSX share a fingerprint", f.FP, fb.statics, t.Statics)
		}
	}
	return t, nil
}

func (b *treeBuilder) items(ctx context.Context, fb *frameBuild, sh *shape, kids []Node, extra int) error {
	for i, n := range kids {
		switch c := sh.next(); c {
		case 't':
			b.toStatic(fb)
			if err := Walk(ctx, n, &b.h); err != nil {
				return err
			}
		case 'h', 'b', 'c':
			if err := b.dyn(ctx, fb, n); err != nil {
				return err
			}
		case 'G':
			sh.next() // (
			if err := b.items(ctx, fb, sh, n.(Group), 0); err != nil {
				return err
			}
			sh.next() // )
		case 'E':
			if i > 0 {
				extra = 0
			}
			if err := b.elem(ctx, fb, sh, n.(*Element), extra); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *treeBuilder) elem(ctx context.Context, fb *frameBuild, sh *shape, el *Element, extra int) error {
	flags := sh.flags()
	void := Void(el.Tag)
	if void && len(el.Children) > 0 {
		return fmt.Errorf("vuka: <%s> is a void element and can't have children", el.Tag)
	}
	if h := liveHost(ctx); h != nil {
		if k, ok := elementKey(el); ok {
			ctx = h.Keyed(ctx, k)
		}
	}
	b.toStatic(fb)
	if err := b.h.openTag(el); err != nil {
		return err
	}
	for i, a := range el.Attrs[:len(flags)] {
		if flags[i] == 's' {
			b.toStatic(fb)
			if err := b.h.attr(el, a); err != nil {
				return err
			}
			continue
		}
		if err := b.dynString(fb, func() error { return b.h.attr(el, a) }); err != nil {
			return err
		}
	}
	if extra > 0 {
		if err := b.dynString(fb, func() error {
			for _, a := range el.Attrs[len(flags):] {
				if err := b.h.attr(el, a); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	b.toStatic(fb)
	if err := b.h.endTag(el); err != nil {
		return err
	}
	sh.next() // (
	if err := b.items(ctx, fb, sh, el.Children, 0); err != nil {
		return err
	}
	sh.next() // )
	if void {
		return nil
	}
	b.toStatic(fb)
	return b.h.Close(el)
}

// dynString adds a dynamic of what write writes.
func (b *treeBuilder) dynString(fb *frameBuild, write func() error) error {
	fb.endStatic()
	var s strings.Builder
	b.h.w = &s
	err := write()
	fb.dyn = append(fb.dyn, s.String())
	return err
}

func (b *treeBuilder) dyn(ctx context.Context, fb *frameBuild, n Node) error {
	fb.endStatic()
	v, err := b.value(ctx, n)
	fb.dyn = append(fb.dyn, v)
	return err
}

// value is a dynamic node's value in the tree.
func (b *treeBuilder) value(ctx context.Context, n Node) (any, error) {
	switch n := n.(type) {
	case nil:
		return "", nil
	case *Frame:
		if n == nil {
			return "", nil
		}
		return b.frame(ctx, n)
	case *ComponentNode:
		if th, ok := liveHost(ctx).(TreeHost); ok && n != nil {
			id, err := th.ComponentRef(ctx, n)
			if id == "" || err != nil {
				return "", err
			}
			return TreeRef(id), nil
		}
	case Builder:
		if n == nil {
			return "", nil
		}
		var ns []Node
		n(func(c Node) { ns = append(ns, c) })
		return b.seq(ctx, ns, true)
	case Group:
		return b.seq(ctx, n, false)
	case TryNode:
		if n.Err != nil {
			return nil, n.Err
		}
		return b.value(ctx, n.Node)
	}
	return b.html(ctx, n)
}

// seq is the value of a sequence of nodes: a block's items as a list, one
// node as itself, frames as a list, anything else as HTML.
func (b *treeBuilder) seq(ctx context.Context, ns []Node, block bool) (any, error) {
	if len(ns) == 1 && !block {
		return b.value(ctx, ns[0])
	}
	frames := len(ns) > 0 || block
	for _, n := range ns {
		if f, ok := n.(*Frame); !ok || f == nil {
			frames = false
			break
		}
	}
	if !frames {
		return b.html(ctx, Group(ns))
	}
	l := &TreeList{Items: make([]*Tree, len(ns)), Keys: make([]string, len(ns))}
	for i, n := range ns {
		f := n.(*Frame)
		t, err := b.frame(ctx, f)
		if err != nil {
			return nil, err
		}
		l.Items[i] = t
		if l.Keys != nil {
			if k, ok := frameKey(f); ok {
				l.Keys[i] = k
			} else {
				l.Keys = nil
			}
		}
	}
	if len(ns) == 0 {
		l.Keys = nil
	}
	return l, nil
}

// frameKey is the key of a block item: its first element's or component
// tag's key.
func frameKey(f *Frame) (string, bool) {
	if len(f.Kids) == 0 {
		return "", false
	}
	switch n := f.Kids[0].(type) {
	case *Element:
		if k, ok := elementKey(n); ok && k != nil {
			return fmt.Sprintf("%T=%v", k, k), true
		}
	case *ComponentNode:
		if n != nil && n.Key != nil {
			return fmt.Sprintf("%T=%v", n.Key, n.Key), true
		}
	}
	return "", false
}

// html renders n as HTML, in the current context.
func (b *treeBuilder) html(ctx context.Context, n Node) (string, error) {
	var s strings.Builder
	saved := b.h.w
	b.h.w = &s
	err := Walk(ctx, n, &b.h)
	b.h.w = saved
	return s.String(), err
}

// shape reads a Frame's Shape.
type shape struct {
	s string
	i int
}

func (sh *shape) done() bool { return sh.i == len(sh.s) }

func (sh *shape) next() byte {
	if sh.i >= len(sh.s) {
		return 0
	}
	c := sh.s[sh.i]
	sh.i++
	return c
}

// flags reads an element's attribute flags.
func (sh *shape) flags() string {
	j := sh.i
	for j < len(sh.s) && (sh.s[j] == 's' || sh.s[j] == 'd') {
		j++
	}
	f := sh.s[sh.i:j]
	sh.i = j
	return f
}

// fits reports whether kids are the nodes the shape describes, reading it.
func (sh *shape) fits(kids []Node, extra int) bool {
	for i, n := range kids {
		switch sh.next() {
		case 't', 'h', 'b', 'c':
		case 'G':
			g, ok := n.(Group)
			if !ok || sh.next() != '(' || !sh.fits(g, 0) || sh.next() != ')' {
				return false
			}
		case 'E':
			el, ok := n.(*Element)
			if i > 0 {
				extra = 0
			}
			if !ok || el == nil || len(el.Attrs) != len(sh.flags())+extra || sh.next() != '(' || !sh.fits(el.Children, 0) || sh.next() != ')' {
				return false
			}
		default:
			return false
		}
	}
	return sh.i == len(sh.s) || sh.s[sh.i] == ')'
}
