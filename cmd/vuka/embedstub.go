package main

import (
	"encoding/json"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Development builds without the frontend's build in them. Under vuka run
// web.Frontend serves its files from disk, never from the embed.FS it is
// given, so the copy the binary embeds is dead weight the linker writes on
// every rebuild. vuka run overlays each embedded file of the trees
// web.Frontend serves with an empty one — only those: the files of a
// pattern its root lies in, under that root; other embeds (app.Static's
// files, templates, data) are left as they are. VUKA_EMBED_STUB=off, or
// -embed, builds them in.

// frontendEmbeds are the files of the module at root that a web.Frontend
// call serves from an embedded tree.
func frontendEmbeds(root string) []string {
	var files []string
	seen := map[string]bool{}
	walkSources(root, func(p, name string) error {
		ext := filepath.Ext(name)
		if ext != ".go" && ext != ".vuka" || strings.HasSuffix(name, "_test"+ext) {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil || !strings.Contains(string(src), `"`+webPath+`"`) {
			return nil
		}
		for _, f := range frontendCalls(src) {
			patterns := embedPatterns(filepath.Dir(p), f.fsVar)
			if !covered(patterns, f.root) {
				continue
			}
			dir := filepath.Join(filepath.Dir(p), filepath.FromSlash(f.root))
			filepath.WalkDir(dir, func(q string, d fs.DirEntry, err error) error {
				if err == nil && d.Type().IsRegular() && !seen[q] {
					seen[q] = true
					files = append(files, q)
				}
				return nil
			})
		}
		return nil
	})
	return files
}

type frontendCall struct{ fsVar, root string }

// frontendCalls are a source's web.Frontend(fsys, "root", …) calls whose
// fsys is a variable.
func frontendCalls(src []byte) []frontendCall {
	full, _ := scanFile("", src)
	toks := dropNewlineSemis(full)
	webName := ""
	for n, p := range fileImports(toks) {
		if p == webPath {
			webName = n
		}
	}
	var out []frontendCall
	for i := 0; i+3 < len(toks); i++ {
		if toks[i].tok != token.IDENT || toks[i].lit != webName || toks[i+1].tok != token.PERIOD ||
			toks[i+2].tok != token.IDENT || toks[i+2].lit != "Frontend" || toks[i+3].tok != token.LPAREN {
			continue
		}
		args := callArgs(toks, i+3)
		if len(args) < 2 || args[0][1]-args[0][0] != 1 || args[1][1]-args[1][0] != 1 ||
			toks[args[0][0]].tok != token.IDENT || toks[args[1][0]].tok != token.STRING {
			continue
		}
		r, err := strconv.Unquote(toks[args[1][0]].lit)
		if err != nil {
			continue
		}
		out = append(out, frontendCall{toks[args[0][0]].lit, strings.Trim(path.Clean("/"+r), "/")})
	}
	return out
}

// embedPatterns are the //go:embed patterns of the embed.FS variable name
// declared in the package in dir.
func embedPatterns(dir, name string) []string {
	re := regexp.MustCompile(`(?m)((?:^[ \t]*//go:embed[^\n]*\n(?:[ \t]*//[^\n]*\n)*)+)[ \t]*var[ \t]+` + regexp.QuoteMeta(name) + `[ \t]+embed\.FS`)
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !(strings.HasSuffix(n, ".go") || strings.HasSuffix(n, ".vuka")) || strings.HasSuffix(n, "_vuka.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		m := re.FindSubmatch(src)
		if m == nil {
			continue
		}
		var pats []string
		for _, line := range strings.Split(string(m[1]), "\n") {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "//go:embed"); ok {
				for _, f := range strings.Fields(rest) {
					if u, err := strconv.Unquote(f); err == nil {
						f = u
					}
					pats = append(pats, f)
				}
			}
		}
		return pats
	}
	return nil
}

// covered reports whether root lies in a pattern (a directory, not a glob).
func covered(patterns []string, root string) bool {
	for _, p := range patterns {
		p = strings.TrimPrefix(p, "all:")
		if strings.ContainsAny(p, "*?[") {
			continue
		}
		p = strings.Trim(path.Clean("/"+p), "/")
		if root == p || strings.HasPrefix(root, p+"/") {
			return true
		}
	}
	return false
}

// stubEmbeds adds files to the overlay (in tmp), each as an empty file.
func stubEmbeds(overlay, tmp string, files []string) (string, error) {
	if len(files) == 0 {
		return overlay, nil
	}
	ov := struct{ Replace map[string]string }{map[string]string{}}
	if overlay != "" {
		data, err := os.ReadFile(overlay)
		if err != nil {
			return "", err
		}
		if err := json.Unmarshal(data, &ov); err != nil {
			return "", err
		}
	} else {
		overlay = filepath.Join(tmp, "overlay.json")
	}
	empty := filepath.Join(tmp, "embed-stub")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		return "", err
	}
	for _, f := range files {
		if _, ok := ov.Replace[f]; !ok {
			ov.Replace[f] = empty
		}
	}
	data, err := json.Marshal(ov)
	if err != nil {
		return "", err
	}
	return overlay, os.WriteFile(overlay, data, 0o644)
}

// stubbing reports whether vuka run leaves the frontend's build out of the
// binary: not with -embed (cut from flags) or VUKA_EMBED_STUB=off.
func stubbing(flags []string) ([]string, bool) {
	out := flags[:0:0]
	on := !strings.EqualFold(os.Getenv("VUKA_EMBED_STUB"), "off")
	for _, f := range flags {
		switch strings.TrimLeft(f, "-") {
		case "embed", "embed=true":
			on = false
		case "embed=false":
		default:
			out = append(out, f)
		}
	}
	return out, on
}
