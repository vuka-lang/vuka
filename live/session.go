// Package live runs stateful components for a connected browser: a Session
// keeps the component instances of one page, calls their event handlers and
// Update methods, and answers each with the HTML of the components whose
// output changed. It knows no transport; a WebSocket server drives it with the
// messages of protocol.go.
package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/vuka-lang/vuka"
)

// Session is one connected page: its render function and the instances of the
// stateful components it renders. Its methods are safe to call from several
// goroutines; each runs alone.
type Session struct {
	mu     sync.Mutex
	ctx    context.Context
	render func(ctx context.Context) vuka.Node
	root   *instance
	byID   map[string]*instance
	next   int
	pass   int
	closed bool
	trees  bool // a v2 connection: instances render to trees
	c      conn
	verify bool
}

// Patch is what an event or message changed: the HTML of each component whose
// output changed, outermost first (a changed component's update includes the
// components inside it), and the error a handler returned.
type Patch struct {
	Updates []Update     `json:"updates,omitempty"`
	Trees   []TreeUpdate `json:"trees,omitempty"`
	Error   string       `json:"error,omitempty"`
}

// TreeUpdate is an instance's change on a v2 connection (see RenderTrees):
// its whole tree (Full), the change to the tree the client holds, or its
// whole HTML when that is smaller. Tree is JSON, encoded as components.md
// ("Protocol v2") describes.
type TreeUpdate struct {
	ID   string          `json:"id"`
	Full bool            `json:"full,omitempty"`
	Tree json.RawMessage `json:"tree,omitempty"`
	HTML string          `json:"html,omitempty"`
}

// Update is a component's new HTML. ID is the data-vk-id of the element it
// replaces; RootID is the page itself, whose HTML replaces the container's
// content.
type Update struct {
	ID   string `json:"id"`
	HTML string `json:"html"`
}

// RootID is the id of the page's own render, which owns the handlers of
// elements outside any stateful component.
const RootID = "c0"

// NewSession is a session rendering the page render returns. ctx is the
// context of Mount, handlers and Update methods, and of every render.
func NewSession(ctx context.Context, render func(ctx context.Context) vuka.Node) *Session {
	s := &Session{ctx: ctx, render: render, byID: map[string]*instance{}, verify: Verify}
	s.root = &instance{s: s, id: RootID, children: map[string]*instance{}}
	s.byID[RootID] = s.root
	return s
}

// Render renders the whole page, mounting the components it meets for the
// first time, and returns its HTML: what the page's container holds.
func (s *Session) Render() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trees = false
	s.refresh()
	if err := s.renderAll(); err != nil {
		return "", err
	}
	s.settle()
	return s.root.html(), nil
}

// RenderTrees makes the session a v2 one and renders the whole page as every
// instance's render tree, the page's first: what a v2 client joins with.
// From then on patches carry Trees instead of Updates. It starts the
// connection afresh: every statics id and kept string is forgotten.
func (s *Session) RenderTrees() ([]TreeUpdate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trees = true
	s.c.reset()
	s.refresh()
	if err := s.renderAll(); err != nil {
		return nil, err
	}
	ups := s.treeUpdates()
	s.settle()
	return ups, nil
}

// refresh makes every instance render again, its output sent whole.
func (s *Session) refresh() {
	for _, in := range s.byID {
		in.fresh, in.shadow = true, nil
	}
}

// HTML is the page's HTML as last rendered, without rendering it again.
func (s *Session) HTML() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.trees {
		var b strings.Builder
		s.root.treeHTML(&b)
		return b.String()
	}
	return s.root.html()
}

// Event calls the handler ev.Target names with the event's payload, renders
// the page again, and returns the components whose output changed. A
// handler's error is the patch's Error; the returned error is the session's:
// an unknown handler, or a render that failed.
func (s *Session) Event(ev vuka.Event) (Patch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, n, ok := strings.Cut(ev.Target, ":")
	in := s.byID[id]
	h, err := strconv.Atoi(n)
	if !ok || in == nil || err != nil || h < 0 || h >= len(in.handlers) {
		return Patch{}, fmt.Errorf("live: no handler %q", ev.Target)
	}
	herr := callHandler(s.ctx, in.handlers[h], ev)
	p, err := s.patch()
	if herr != nil {
		p.Error = herr.Error()
	}
	return p, err
}

// Info delivers msg to the Update method fitting its type of every instance
// subscribed to topic (of every instance, for ""), renders the page again if
// any took it, and returns what changed. An Update's error is the patch's Error.
func (s *Session) Info(topic string, msg any) (Patch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var herr error
	called := false
	for _, in := range s.order() {
		if in.comp == nil || topic != "" && !slices.Contains(in.topics, topic) {
			continue
		}
		ok, err := callUpdate(s.ctx, in.comp, msg)
		called = called || ok
		herr = errors.Join(herr, err)
	}
	if !called {
		return Patch{}, herr
	}
	p, err := s.patch()
	if herr != nil {
		p.Error = herr.Error()
	}
	return p, err
}

// Topics is every topic an instance subscribes to, sorted: what the transport
// routes to Info. It changes as instances come and go.
func (s *Session) Topics() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ts []string
	for _, in := range s.byID {
		for _, t := range in.topics {
			if !slices.Contains(ts, t) {
				ts = append(ts, t)
			}
		}
	}
	slices.Sort(ts)
	return ts
}

// Close unmounts every instance. The session renders nothing after it.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, in := range s.order() {
		unmount(in)
	}
	s.byID, s.closed = map[string]*instance{}, true
}

// instance is a stateful component in the session, or the page itself (comp nil).
type instance struct {
	s        *Session
	id       string
	comp     vuka.Stateful
	typ      reflect.Type
	children map[string]*instance // by place: key path, site, key or occurrence
	kids     []*instance          // in render order, this pass
	occ      map[string]int
	handlers []any
	topics   []string
	seen     int
	own      string // its HTML, each child as a marker
	changed  bool
	fresh    bool
	key      any // its tag's key, written on its root as data-vk-key

	tree     *vuka.Tree // its last render, on a v2 connection
	shadow   *sframe    // what the client holds of it; nil: nothing yet
	rendered bool       // rendered this pass (not skipped)
}

func (in *instance) ID() string { return in.id }

func (in *instance) Subscribe(topics ...string) {
	for _, t := range topics {
		if !slices.Contains(in.topics, t) {
			in.topics = append(in.topics, t)
		}
	}
}

// scope is where a render is: the instance whose output it is, and the keys
// of the elements around it.
type scope struct {
	in   *instance
	keys string
}

func (sc *scope) Handler(_ context.Context, fn any) string {
	sc.in.handlers = append(sc.in.handlers, fn)
	return sc.in.id + ":" + strconv.Itoa(len(sc.in.handlers)-1)
}

func (sc *scope) Keyed(ctx context.Context, key any) context.Context {
	return vuka.WithLiveHost(ctx, &scope{sc.in, sc.keys + keyText(key) + "/"})
}

func keyText(k any) string { return vuka.KeyText(k) }

// Component renders a stateful component's tag: the instance at its place, or
// a new one, mounted, with the tag's props; its HTML is a marker the
// session replaces.
func (sc *scope) Component(ctx context.Context, n *vuka.ComponentNode, w io.Writer) error {
	in, err := sc.place(n)
	if in == nil || err != nil {
		return err
	}
	_, err = io.WriteString(w, marker(in.id))
	return err
}

// ComponentRef renders a stateful component's tag in a render tree: its
// instance's id.
func (sc *scope) ComponentRef(ctx context.Context, n *vuka.ComponentNode) (string, error) {
	in, err := sc.place(n)
	if in == nil || err != nil {
		return "", err
	}
	return in.id, nil
}

// place finds (or mounts) the instance at a tag's place, sets its props and
// renders it, unless Assign tracking says its output can't have changed.
func (sc *scope) place(n *vuka.ComponentNode) (*instance, error) {
	if n.Props == nil {
		return nil, nil
	}
	s, parent := sc.in.s, sc.in
	place := sc.keys + n.Site + "#"
	if n.Key != nil {
		place += "k" + keyText(n.Key)
	} else {
		parent.occ[place]++
		place += strconv.Itoa(parent.occ[place])
	}
	typ := reflect.TypeOf(n.Props)
	in := parent.children[place]
	same := false
	switch {
	case in != nil && in.seen == s.pass && in.typ == typ:
		return nil, fmt.Errorf("live: two <%s> in one place share the key %v", typ.Elem().Name(), n.Key)
	case in == nil || in.typ != typ:
		s.next++
		in = &instance{s: s, id: "c" + strconv.Itoa(s.next), typ: typ, comp: vuka.CopyLive(n.Props), children: map[string]*instance{}, fresh: true}
		vuka.AttachLive(in.comp, in)
		if err := vuka.MountLive(s.ctx, in.comp); err != nil {
			return nil, err
		}
		parent.children[place] = in
		s.byID[in.id] = in
	default:
		same = sameProps(in.comp, n.Props)
		copyProps(in.comp, n.Props)
	}
	in.key = n.Key
	parent.kids = append(parent.kids, in)
	return in, s.visit(in, same)
}

func marker(id string) string { return "\x00vk:" + id + "\x00" }

// copyProps sets the exported fields of to from from: a parent's render sets
// every prop again.
func copyProps(to, from vuka.Stateful) {
	dst, src := reflect.ValueOf(to).Elem(), reflect.ValueOf(from).Elem()
	live := reflect.TypeFor[vuka.Live]()
	for i := 0; i < dst.NumField(); i++ {
		if f := dst.Type().Field(i); f.IsExported() && f.Type != live {
			dst.Field(i).Set(src.Field(i))
		}
	}
}

func (s *Session) renderAll() error {
	if s.closed {
		return errors.New("live: the session is closed")
	}
	s.pass++
	if err := s.renderInst(s.root); err != nil {
		return err
	}
	for id, in := range s.byID {
		if in.seen != s.pass {
			unmount(in)
			delete(s.byID, id)
		}
	}
	for _, in := range s.byID {
		for place, c := range in.children {
			if c.seen != s.pass {
				delete(in.children, place)
			}
		}
	}
	return nil
}

// visit renders an instance, or keeps its last render when its state is all
// Assigns, none changed, and its props are the same (same).
func (s *Session) visit(in *instance, same bool) error {
	if same && !in.fresh {
		if tracked, changed := vuka.TrackedState(in.comp); tracked && !changed {
			if s.verify {
				return s.verifySkip(in)
			}
			return s.keep(in)
		}
	}
	return s.renderInst(in)
}

// keep keeps an instance's last render and visits the instances it rendered,
// whose props it would have set the same.
func (s *Session) keep(in *instance) error {
	in.seen, in.rendered, in.changed = s.pass, false, false
	for _, k := range in.kids {
		if err := s.visit(k, true); err != nil {
			return err
		}
	}
	return nil
}

// verifySkip renders an instance keep would have kept, and fails if its
// output differs from the last.
func (s *Session) verifySkip(in *instance) error {
	own, tree := in.own, in.tree
	if err := s.renderInst(in); err != nil {
		return err
	}
	if s.trees && !sameTree(tree, in.tree) || !s.trees && own != in.own {
		return fmt.Errorf("live: %s (%s) renders differently though its Assigns and props didn't change: does Render read a field that isn't an Assign or a prop? (VUKA_LIVE_VERIFY)", in.typ.Elem().Name(), in.id)
	}
	return nil
}

func (s *Session) renderInst(in *instance) error {
	in.seen, in.rendered = s.pass, true
	in.handlers, in.kids, in.occ = in.handlers[:0], in.kids[:0], map[string]int{}
	ctx := vuka.WithLiveHost(s.ctx, &scope{in: in})
	var node vuka.Node
	if in.comp == nil {
		node = s.render(ctx)
	} else {
		node = rooted(in.comp.Render(), in.id, in.key)
	}
	if in.comp != nil {
		defer vuka.SettleState(in.comp)
	}
	if s.trees {
		t, err := vuka.BuildTree(ctx, node, s.verify)
		if err != nil {
			return err
		}
		in.tree = splitMarkers(t)
		return nil
	}
	var b strings.Builder
	if node != nil {
		if err := node.Render(ctx, &b); err != nil {
			return err
		}
	}
	own := b.String()
	in.changed = in.fresh || own != in.own
	in.own = own
	return nil
}

// rooted is a component's output with its id, and its tag's key, on the root
// element, or on a <vk-c> around it when it isn't one element.
func rooted(n vuka.Node, id string, key any) vuka.Node {
	attrs := []vuka.Attr{{Name: "data-vk-id", Value: id}}
	if key != nil {
		attrs = append(attrs, vuka.Attr{Name: "data-vk-key", Value: fmt.Sprint(key)})
	}
	if r, ok := vuka.RootAttrs(n, attrs...); ok {
		return r
	}
	return vuka.El("vk-c", append(attrs, vuka.Attr{Name: "style", Value: "display:contents"}), n)
}

// html is an instance's HTML with its children's in place of their markers.
func (in *instance) html() string {
	if !strings.Contains(in.own, "\x00vk:") {
		return in.own
	}
	var b strings.Builder
	rest := in.own
	for {
		i := strings.Index(rest, "\x00vk:")
		if i < 0 {
			b.WriteString(rest)
			return b.String()
		}
		b.WriteString(rest[:i])
		rest = rest[i+4:]
		j := strings.IndexByte(rest, 0)
		if j < 0 {
			b.WriteString(rest)
			return b.String()
		}
		if c := in.s.byID[rest[:j]]; c != nil && c != in {
			b.WriteString(c.html())
		}
		rest = rest[j+1:]
	}
}

// patch renders the page again and collects the outermost changed instances.
func (s *Session) patch() (Patch, error) {
	if err := s.renderAll(); err != nil {
		return Patch{}, err
	}
	var p Patch
	if s.trees {
		p.Trees = s.treeUpdates()
		s.settle()
		return p, nil
	}
	var walk func(in *instance)
	walk = func(in *instance) {
		if in.changed {
			p.Updates = append(p.Updates, Update{in.id, in.html()})
			return
		}
		for _, c := range in.kids {
			walk(c)
		}
	}
	walk(s.root)
	s.settle()
	return p, nil
}

func (s *Session) settle() {
	for _, in := range s.byID {
		in.changed, in.fresh, in.rendered = false, false, false
	}
}

// order is the instances in render order, the page first.
func (s *Session) order() []*instance {
	var out []*instance
	var walk func(in *instance)
	walk = func(in *instance) {
		out = append(out, in)
		for _, c := range in.kids {
			walk(c)
		}
	}
	walk(s.root)
	return out
}

func unmount(in *instance) {
	if u, ok := in.comp.(interface{ Unmount() }); ok {
		u.Unmount()
	}
}
