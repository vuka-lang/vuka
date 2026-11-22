//go:build vuka_release

package vuka

import "reflect"

// reflectMethods finds none: a program built with the vuka_release tag finds
// methods in the tables Vuka generates only, so the linker can drop the
// methods it never calls.
func reflectMethods(reflect.Type) ([]Method, bool) { return nil, false }
