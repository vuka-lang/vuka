// Package describe prints what a golden model's Schema method declares.
package describe

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"vukatest.local/orm"
)

// Model is what T's Schema method declares.
func Model[T any]() string {
	x := reflect.New(reflect.TypeFor[T]())
	s := orm.NewSchema(x.Interface())
	x.Interface().(interface{ Schema(*orm.Schema) }).Schema(s)
	var names []string
	for n := range s.Attrs() {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	fmt.Fprintf(&b, "%s table=%q", reflect.TypeFor[T]().Name(), s.Table())
	for _, n := range names {
		fmt.Fprintf(&b, " %s=%v", n, s.Attrs()[n])
	}
	return b.String()
}
