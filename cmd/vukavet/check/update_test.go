package check

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

func TestIsUpdate(t *testing.T) {
	src := `package p

import "context"

type Msg struct{}
type List[T any] struct{}
type Chat struct{}

func (*Chat) UpdateMsg(m Msg)                         {}
func (*Chat) UpdateMsgs(ctx context.Context, m []*Msg) {}
func (*Chat) UpdateListMsg(m List[Msg])               {}
func (*Chat) UpdateMapStringInt(m map[string]int)     {}
func (*Chat) UpdateAny(m any)                         {}
func (*Chat) Update__Msg(m Msg)                       {}
func (*Chat) UpdateTopic(topic string)                {}
func (*Chat) UpdateMsg2(m Msg)                        {}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := (&types.Config{Importer: importer.Default()}).Check("p", fset, []*ast.File{f}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ms := types.NewMethodSet(types.NewPointer(pkg.Scope().Lookup("Chat").Type()))
	want := map[string]bool{"UpdateMsg": true, "UpdateMsgs": true, "UpdateListMsg": true, "UpdateMapStringInt": true,
		"UpdateAny": true, "Update__Msg": true, "UpdateTopic": false, "UpdateMsg2": false}
	for i := range ms.Len() {
		m := ms.At(i).Obj().(*types.Func)
		if got := isUpdate(m); got != want[m.Name()] {
			t.Errorf("isUpdate(%s) = %v", m.Name(), got)
		}
	}
}
