package load

import (
	"errors"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// typedOf is Options.Facts' check of p: its Go as the run has it, its
// imports through imp. It tolerates type errors, as a package with some
// still declares what it declares.
func typedOf(p *Package, imp types.Importer, sess *Session, replace map[string]string, root string) func(skip ...string) *Typed {
	return func(skip ...string) *Typed {
		ctx := replaceContext(replace)
		goVersion := goDirective(root)
		var sizes types.Sizes
		if sess != nil {
			ctx, goVersion, sizes = sess.buildContext(), sess.goVersion, sess.sizes
		}
		dir := p.BuildDir()
		bp, err := ctx.ImportDir(dir, 0)
		if bp == nil || err != nil && len(bp.GoFiles) == 0 {
			return nil
		}
		fset := token.NewFileSet()
		var files []*ast.File
		for _, name := range bp.GoFiles {
			if name == RouteGoName || slices.Contains(skip, name) {
				continue
			}
			rc, err := ctx.OpenFile(filepath.Join(dir, name))
			if err != nil {
				continue
			}
			src, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				continue
			}
			if f, _ := parser.ParseFile(fset, filepath.Join(dir, name), src, parser.SkipObjectResolution); f != nil {
				files = append(files, f)
			}
		}
		if len(files) == 0 {
			return nil
		}
		uses := map[*ast.Ident]types.Object{}
		conf := types.Config{Importer: imp, GoVersion: goVersion, Sizes: sizes, Error: func(error) {}}
		pkg, _ := conf.Check(p.ImportPath, fset, files, &types.Info{Uses: uses})
		if pkg == nil {
			return nil
		}
		return &Typed{Pkg: pkg, Fset: fset, Files: files, Uses: uses, Import: imp.Import}
	}
}

// replaceContext is the build context seeing an overlay's replacements.
func replaceContext(replace map[string]string) *build.Context {
	ctx := build.Default
	ctx.ReadDir = func(dir string) ([]fs.FileInfo, error) {
		entries, err := ReadDir(dir)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		var infos []fs.FileInfo
		have := map[string]bool{}
		for _, e := range entries {
			path := filepath.Join(dir, e.Name())
			have[e.Name()] = true
			if r, ok := replace[path]; ok {
				if r == "" {
					continue
				}
				if fi, err := os.Stat(r); err == nil {
					infos = append(infos, genInfo{e.Name(), fi.Size()})
				}
				continue
			}
			if fi, err := e.Info(); err == nil {
				infos = append(infos, fi)
			}
		}
		for target, r := range replace {
			if filepath.Dir(target) == dir && !have[filepath.Base(target)] && r != "" {
				if fi, err := os.Stat(r); err == nil {
					infos = append(infos, genInfo{filepath.Base(target), fi.Size()})
				}
			}
		}
		slices.SortFunc(infos, func(a, b fs.FileInfo) int { return strings.Compare(a.Name(), b.Name()) })
		return infos, nil
	}
	ctx.IsDir = func(path string) bool {
		if fi, err := os.Stat(path); err == nil {
			return fi.IsDir()
		}
		for target := range replace {
			if strings.HasPrefix(target, path+string(filepath.Separator)) {
				return true
			}
		}
		return false
	}
	ctx.OpenFile = func(path string) (io.ReadCloser, error) {
		if r, ok := replace[path]; ok {
			if r == "" {
				return nil, os.ErrNotExist
			}
			path = r
		}
		return os.Open(path)
	}
	return &ctx
}
