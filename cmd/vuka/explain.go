package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vuka-lang/vuka/internal/load"
	"github.com/vuka-lang/vuka/transpile"
)

// explain shows what Vuka makes of a file: each rewritten line beside the Go
// it became, then the code Vuka adds after the source, each labelled with the
// line it comes from.
func explain(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("explain", flag.ContinueOnError)
	full := fs.Bool("full", false, "print the whole generated Go file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 || !strings.HasSuffix(fs.Arg(0), ".vuka") {
		return errors.New("usage: vuka explain [-full] file.vuka")
	}
	path, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return err
	}
	if r, err := filepath.EvalSymlinks(path); err == nil {
		path = r
	}
	out, src, err := generate(path)
	if err != nil {
		return err
	}
	if *full {
		_, err := stdout.Write(out.Src)
		return err
	}
	writeExplanation(stdout, filepath.Base(path), src, out)
	return nil
}

// generate transpiles the module and returns path's generated Go, bare: no
// header or line directives, so its lines are the source's lines.
func generate(path string) (transpile.Output, []byte, error) {
	root, modPath, err := load.ModuleRoot(filepath.Dir(path))
	if err != nil {
		return transpile.Output{}, nil, err
	}
	pkgs, err := load.Discover(root, modPath, root, true, nil)
	if err != nil {
		return transpile.Output{}, nil, err
	}
	tmp, err := os.MkdirTemp("", "vuka-")
	if err != nil {
		return transpile.Output{}, nil, err
	}
	defer os.RemoveAll(tmp)
	// Transpile everything (for imports between Vuka packages), bare, then
	// once more this package alone to get its maps.
	if _, _, err := load.Transpile(pkgs, tmp, load.Options{Bare: true}); err != nil {
		return transpile.Output{}, nil, err
	}
	for _, p := range pkgs {
		if p.Dir != filepath.Dir(path) {
			continue
		}
		for _, f := range p.Files {
			if filepath.Join(p.Dir, f.Name) != path {
				continue
			}
			res, err := transpile.Package(p.Files, transpile.Options{
				Bare:       true,
				Importer:   load.NewImporter(p.Dir, filepath.Join(tmp, "overlay.json")),
				Path:       func(name string) string { return filepath.Join(p.Dir, name) },
				Dir:        p.Dir,
				ImportPath: p.ImportPath,
			})
			if err != nil {
				return transpile.Output{}, nil, err
			}
			for _, o := range res.Files {
				if o.Name == f.Name {
					return o, f.Src, nil
				}
			}
		}
	}
	return transpile.Output{}, nil, fmt.Errorf("%s isn't in a package of this module", path)
}

type explained struct {
	line   int // 1-based source line it's about
	after  bool
	source []string
	gen    []string
	note   string
}

func writeExplanation(w io.Writer, name string, src []byte, out transpile.Output) {
	srcLines := strings.Split(string(src), "\n")
	genBody := strings.Split(string(out.Src[:out.Body]), "\n")
	lineOf := func(text []byte, off int) int { return bytes.Count(text[:min(off, len(text))], []byte("\n")) + 1 }

	// Lines of the source Vuka rewrote: the body keeps the source's lines.
	touched := map[int]bool{}
	var trailer []transpile.Piece
	for _, p := range out.Map.Pieces() {
		switch {
		case p.Gen >= out.Body:
			trailer = append(trailer, p)
		case !p.Copied && (p.SrcLen > 0 || p.GenLen > 0):
			for l := lineOf(src, p.Src); l <= lineOf(src, p.Src+max(p.SrcLen-1, 0)); l++ {
				touched[l] = true
			}
		}
	}
	var items []explained
	var lines []int
	for l := range touched {
		lines = append(lines, l)
	}
	sort.Ints(lines)
	for i := 0; i < len(lines); {
		j := i
		for j+1 < len(lines) && lines[j+1] == lines[j]+1 {
			j++
		}
		e := explained{line: lines[i]}
		moved := true
		for l := lines[i]; l <= lines[j]; l++ {
			e.source = append(e.source, at(srcLines, l))
			g := at(genBody, l)
			e.gen = append(e.gen, g)
			if t := strings.TrimSpace(g); t != "" && !strings.HasPrefix(t, "//") && !strings.HasPrefix(t, "/*") {
				moved = false
			}
		}
		if moved {
			e.gen, e.note = nil, "becomes a comment here; its Go is below"
		}
		items = append(items, e)
		i = j + 1
	}

	// Code Vuka adds after the source, in blocks separated by blank lines.
	tail := string(out.Src[out.Body:])
	pos := out.Body
	for _, block := range strings.Split(tail, "\n\n") {
		start := pos
		pos += len(block) + 2
		if strings.TrimSpace(block) == "" {
			continue
		}
		srcOff := -1
		for _, p := range trailer {
			if p.Gen < start+len(block) && p.Gen+p.GenLen > start && (srcOff < 0 || p.Src < srcOff) {
				srcOff = p.Src
			}
		}
		if srcOff < 0 {
			continue
		}
		l := lineOf(src, srcOff)
		items = append(items, explained{line: l, after: true, source: []string{at(srcLines, l)},
			gen: strings.Split(strings.Trim(block, "\n"), "\n")})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].line != items[j].line {
			return items[i].line < items[j].line
		}
		return !items[i].after && items[j].after
	})

	if len(items) == 0 {
		fmt.Fprintf(w, "%s is plain Go: Vuka leaves it as it is.\n", name)
		return
	}
	for _, e := range items {
		if e.after {
			fmt.Fprintf(w, "%s:%d  added after the code, for: %s\n", name, e.line, strings.TrimSpace(e.source[0]))
		} else {
			fmt.Fprintf(w, "%s:%d\n", name, e.line)
			for _, l := range e.source {
				fmt.Fprintf(w, "  vuka │ %s\n", l)
			}
		}
		if e.note != "" {
			fmt.Fprintf(w, "  go   │ (%s)\n", e.note)
		}
		for _, l := range e.gen {
			fmt.Fprintf(w, "  go   │ %s\n", l)
		}
		fmt.Fprintln(w)
	}
}

func at(lines []string, n int) string {
	if n-1 < len(lines) && n >= 1 {
		return strings.ReplaceAll(lines[n-1], "\t", "    ")
	}
	return ""
}
