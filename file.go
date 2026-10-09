package vuka

import (
	"os"
	"strings"
	"sync"
)

// File names a file of the program's source tree. A string literal written
// where a decorator or typed attribute takes a File is checked when the
// program is compiled (it must exist, in the package's directory or below) and
// embedded into the binary; the File then holds "importpath:relative/path" and
// Bytes returns the embedded content:
//
//	func Template(view vuka.File) func(*vuka.Decl) { … view.Bytes() … }
//
//	@Template("views/pet.html")
//	func showPet(id int) Pet { … }
//
// For a .templ file of the package, TemplComponents also gives its components.
type File string

var (
	embedded sync.Map // File → string
	templs   sync.Map // File → map[string]any
)

// FileOf is an embedded file's File. Generated code calls it.
func FileOf(key, data string) File {
	embedded.Store(File(key), data)
	return File(key)
}

// RegisterTempl records the components compiled from a .templ file. Generated
// code calls it.
func RegisterTempl(key string, components map[string]any) { templs.Store(File(key), components) }

// Pkg is the import path of the package the file belongs to; "" for a File
// Vuka didn't embed.
func (f File) Pkg() string {
	pkg, _, ok := strings.Cut(string(f), ":")
	if !ok {
		return ""
	}
	return pkg
}

// Path is the file's path, relative to its package's directory.
func (f File) Path() string {
	if _, rel, ok := strings.Cut(string(f), ":"); ok {
		return rel
	}
	return string(f)
}

// Bytes is the file's content: embedded, or for a File Vuka didn't embed (a
// conversion such as vuka.File("x.txt")) read from disk.
func (f File) Bytes() ([]byte, error) {
	if data, ok := embedded.Load(f); ok {
		return []byte(data.(string)), nil
	}
	return os.ReadFile(f.Path())
}

// TemplComponents are the components of an embedded .templ file, by name: each
// value is the generated function, func(params…) templ.Component.
func TemplComponents(f File) (map[string]any, bool) {
	m, ok := templs.Load(f)
	if !ok {
		return nil, false
	}
	return m.(map[string]any), true
}
