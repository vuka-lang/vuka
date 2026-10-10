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
// A .templ file may be in a subdirectory that is a package of its own
// (views/pets.templ with `package views`): its File is then that package's
// ("module/views:pets.templ"). With templ and github.com/vuka-lang/ui, the
// file's components are registered too: templx.Components(f).
type File string

var embedded sync.Map // File → string

// FileOf is an embedded file's File. Generated code calls it.
func FileOf(key, data string) File {
	embedded.Store(File(key), data)
	return File(key)
}

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
