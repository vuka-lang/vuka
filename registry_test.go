package vuka

import (
	"reflect"
	"testing"
)

type perm string

type regBoard struct{ n int }

func (b *regBoard) Clear()    {}
func (b regBoard) Count() int { return b.n }
func (b *regBoard) Plain()    {}
func (b *regBoard) Handlers() []func() {
	return []func(){func() { b.n++ }, b.Clear}
}

type regOther struct{}

func (regOther) Clear() {}

func regCharge(int) error { return nil }

type regMethodOnly struct{}

func (*regMethodOnly) Go()    {}
func (*regMethodOnly) Other() {}

func init() {
	RegisterType[regBoard](perm("board.read"))
	RegisterFunc((*regBoard).Clear, perm("board.delete"))
	RegisterFunc(regBoard.Count, perm("board.count"))
	RegisterFunc(regCharge, perm("pay"))
	RegisterFunc((*regMethodOnly).Go, perm("go"))
}

func TestRegistry(t *testing.T) {
	bt := reflect.TypeFor[regBoard]()
	if a := TypeAttrs(bt); len(a) != 1 || a[0] != perm("board.read") {
		t.Errorf("TypeAttrs: %v", a)
	}
	if a := TypeAttrs(reflect.TypeFor[*regBoard]()); len(a) != 1 {
		t.Errorf("TypeAttrs of the pointer: %v", a)
	}
	var p perm
	if a := MethodAttrs(bt, "Clear"); !a.Attr(&p) || p != "board.delete" {
		t.Errorf("MethodAttrs: %v", a)
	}
	if a := MethodAttrs(reflect.TypeFor[*regBoard](), "Count"); len(a) != 1 || a[0] != perm("board.count") {
		t.Errorf("MethodAttrs of a value method through the pointer: %v", a)
	}
	if a := MethodAttrs(bt, "Plain"); a != nil {
		t.Errorf("an undeclared method: %v", a)
	}
	if a := MethodAttrs(reflect.TypeFor[regOther](), "Clear"); a != nil {
		t.Errorf("another type's method of the same name: %v", a)
	}

	// A method's registration makes its type known.
	if r, _ := FuncOf((&regMethodOnly{}).Go); r.Recv != reflect.TypeFor[regMethodOnly]() || r.Method != "Go" {
		t.Errorf("a type known by its method: %+v", r)
	}
	if r, _ := FuncOf((&regMethodOnly{}).Other); r.Recv != reflect.TypeFor[regMethodOnly]() || r.Attrs != nil {
		t.Errorf("its other method: %+v", r)
	}

	b := &regBoard{}
	hs := b.Handlers()
	for _, c := range []struct {
		name   string
		fn     any
		attr   any
		method string
		recv   reflect.Type
		lit    bool
	}{
		{"method value", b.Clear, perm("board.delete"), "Clear", bt, false},
		{"method value from a method", hs[1], perm("board.delete"), "Clear", bt, false},
		{"method expression", (*regBoard).Clear, perm("board.delete"), "Clear", bt, false},
		{"value method value", b.Count, perm("board.count"), "Count", bt, false},
		{"undeclared method", b.Plain, nil, "Plain", bt, false},
		{"function", regCharge, perm("pay"), "", nil, false},
		{"literal", hs[0], nil, "", nil, true},
		{"literal here", func() {}, nil, "", nil, true},
	} {
		for range 2 { // the second from the cache
			r, err := FuncOf(c.fn)
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			if r.Literal != c.lit || r.Method != c.method || r.Recv != c.recv {
				t.Errorf("%s: %+v", c.name, r)
			}
			if c.attr == nil && len(r.Attrs) > 0 || c.attr != nil && (len(r.Attrs) != 1 || r.Attrs[0] != c.attr) {
				t.Errorf("%s: attrs %v", c.name, r.Attrs)
			}
		}
	}
	if _, err := FuncOf(nil); err != ErrNotFunc {
		t.Errorf("nil: %v", err)
	}
	if _, err := FuncOf(3); err != ErrNotFunc {
		t.Errorf("an int: %v", err)
	}
	// A method value made by reflection names no declaration.
	if r, _ := FuncOf(reflect.ValueOf(b).MethodByName("Clear").Interface()); r.Attrs != nil {
		t.Errorf("reflect's method value: %+v", r)
	}
}

func TestParseFuncName(t *testing.T) {
	for in, want := range map[string][4]string{
		"main.F":                            {"main.F", "", ""},
		"main.(*T).M-fm":                    {"main.T.M", "main.T", "M"},
		"main.T.M":                          {"main.T.M", "main.T", "M"},
		"gopkg.in/yaml%2ev3.(*Node).Kind":   {"gopkg.in/yaml%2ev3.Node.Kind", "gopkg.in/yaml%2ev3.Node", "Kind"},
		"example.com/a.b/c.(*Box[...]).Get": {"example.com/a.b/c.Box.Get", "example.com/a.b/c.Box", "Get"},
		"main.(*T).Render.func1":            {"", "", "", "lit"},
		"main.glob..func1":                  {"", "", "", "lit"},
		"main.main.gowrap1":                 {"", "", "", "lit"},
	} {
		k, r, m, lit := parseFuncName(in)
		if k != want[0] || r != want[1] || m != want[2] || lit != (want[3] == "lit") {
			t.Errorf("%s: %q %q %q %v", in, k, r, m, lit)
		}
	}
	if got := escapePath("gopkg.in/yaml.v3"); got != "gopkg.in/yaml%2ev3" {
		t.Errorf("escapePath: %s", got)
	}
}

func TestCallAttrs(t *testing.T) {
	fn := &Func{Name: "x", ErrIndex: -1, CtxIndex: -1, Zero: func() []any { return nil }, Attrs: []any{perm("a")}}
	c := NewCall(fn, nil, nil, nil, func(*Call) []any { return nil })
	if a := c.Attrs(); len(a) != 1 || a[0] != perm("a") {
		t.Errorf("Call.Attrs: %v", a)
	}
}

func regShow(id int, tab string) error { return nil }

func (b *regBoard) Move(from, to int) {}

func TestRegisterParams(t *testing.T) {
	RegisterParams(regShow, "id", "tab")
	RegisterParams((*regBoard).Move, "from", "to")
	if r, _ := FuncOf(regShow); !reflect.DeepEqual(r.Params, []string{"id", "tab"}) {
		t.Errorf("a function's: %q", r.Params)
	}
	b := &regBoard{}
	for _, fn := range []any{(*regBoard).Move, b.Move} {
		if r, _ := FuncOf(fn); !reflect.DeepEqual(r.Params, []string{"from", "to"}) {
			t.Errorf("%T: %q", fn, r.Params)
		}
	}
	if r, _ := FuncOf(regCharge); r.Params != nil {
		t.Errorf("unrecorded: %q", r.Params)
	}
	d := DeclOf(regShow)
	if d.Params[0].Name != "id" || d.Params[1].Name != "tab" {
		t.Errorf("DeclOf: %+v", d.Params)
	}
	d = DeclOf(regShow, Param{Name: "x"})
	if d.Params[0].Name != "x" || d.Params[1].Name != "" {
		t.Errorf("DeclOf with params given: %+v", d.Params)
	}
}
