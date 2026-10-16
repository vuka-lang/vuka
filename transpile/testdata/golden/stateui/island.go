package stateui

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/vuka-lang/vuka"
)

// The island API in the shape the compiler lowers imported islands to; a
// host renders its props JSON and its options as text, enough to see what
// the compiler wrote.

type JSChain interface{ jsChain() Commands }

func (c Commands) jsChain() Commands { return c }

var islands = map[reflect.Type]string{}

func RegisterIsland[T any](path string, strategy ...string) {
	islands[reflect.TypeFor[T]()] = path
}

type IslandOpt func(*[]string)

func Load() IslandOpt             { return opt("load") }
func Visible(...string) IslandOpt { return opt("visible") }
func Once() IslandOpt             { return opt("once") }
func Slot(n Node) IslandOpt       { return func(o *[]string) { *o = append(*o, "slot") } }
func Fallback(Node) IslandOpt     { return opt("fallback") }
func Emit(name string, fn any) IslandOpt {
	return opt("emit " + name)
}
func Callback(prop string, fn any) IslandOpt {
	if c, ok := fn.(Commands); ok {
		return opt("callback " + prop + " = " + c.String())
	}
	return opt(fmt.Sprintf("callback %s = %T", prop, fn))
}
func StateProp[T any](prop string, s State[T]) IslandOpt {
	return opt("state " + prop + " = " + s.String())
}
func Ptr[T any](v T) *T { return &v }

func opt(s string) IslandOpt { return func(o *[]string) { *o = append(*o, s) } }

func MountIsland(site string, props any, opts ...IslandOpt) Node {
	var os []string
	for _, o := range opts {
		o(&os)
	}
	v := reflect.ValueOf(props)
	m := map[string]any{}
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if f.Type.Kind() == reflect.Func || f.Name == "Children" {
			continue
		}
		name, o, _ := strings.Cut(f.Tag.Get("json"), ",")
		if strings.Contains(o, "omitempty") && v.Field(i).IsZero() {
			continue
		}
		fv := v.Field(i)
		if fv.Kind() == reflect.Pointer && !fv.IsNil() {
			fv = fv.Elem()
		}
		m[name] = fv.Interface()
	}
	j, _ := json.Marshal(m)
	return El("vk-island", []Attr{{Name: "data-vk-island", Value: islands[v.Type()]}, {Name: "data-vk-props", Value: string(j)}, {Name: "opts", Value: strings.Join(os, "; ")}})
}

// Live is the embedded field of a live component.
type Live struct{}

// Island is the @ui.Island type decorator.
func Island(args ...string) func(*vuka.Type) {
	return func(t *vuka.Type) {
		islands[t.Reflect] = "(inferred)"
		if len(args) > 0 && args[0] != "" {
			islands[t.Reflect] = args[0]
		}
	}
}

// IslandPage renders a page backed by its island.
func IslandPage(x any) Node {
	v := reflect.ValueOf(x).Elem()
	return El("vk-island", []Attr{{Name: "data-vk-island", Value: islands[v.Type()]}, {Name: "page", Value: v.Type().Name()}})
}
