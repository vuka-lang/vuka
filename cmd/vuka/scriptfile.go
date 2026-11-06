package main

import (
	"os"
	"path/filepath"

	"github.com/vuka-lang/vuka/v2/toolchain/format"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// parseShape is the struct a template file renders: declared in its script,
// or, for a template of markup alone, in the script file beside it
// (orders.go, orders.vuka).
func parseShape(file string, src []byte) (*transpile.Shape, *transpile.Error) {
	sh, err := transpile.ParseShapeFile(file, src)
	if sh != nil || err != nil {
		return sh, err
	}
	for _, name := range transpile.ScriptFileNames(file) {
		if b, rerr := os.ReadFile(name); rerr == nil {
			return transpile.ParseShapeScript(file, src, transpile.File{Name: name, Src: b})
		}
	}
	return nil, nil
}

// templatePackage is the package a template file's formatting gives it
// when it has no package clause: its directory's, or none for a file of
// markup alone whose script file beside it declares its struct.
func templatePackage(path string, src []byte) string {
	if sh, _ := parseShape(path, src); sh != nil && sh.File != "" {
		return ""
	}
	return format.DirPackage(filepath.Dir(path))
}
