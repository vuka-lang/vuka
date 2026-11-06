package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// W0602: a component (a .vuka.html struct) that takes Attrs or Class and
// doesn't pass them on. The attributes a caller writes that the component
// doesn't declare — onClick, id, aria-* — fall through to its Attrs, which
// must be spread on the element its events are on (its root when it has
// none); its Class must be the last of the classes merged (cn, a
// ui.Variants' Class as "class"), so the caller's win. The registry's
// components are checked as the registry is built (TestUIRegistry), and
// vuka vet checks the app's vendored ones.

var (
	tagSpread  = regexp.MustCompile(`\{\s*((?:\w+\.)?\w+(?:\(\))?)\.\.\.\s*\}`)
	classLast  = regexp.MustCompile(`(?:\bcn|MergeClasses|\.Class)\((?:[^()]|\([^()]*\))*,\s*%s\s*\)`)
	eventSpred = regexp.MustCompile(`^(?:\w+\.)?on\w*\(\)$`)
)

// forwardingDiags are W0602 of the component file's struct.
func forwardingDiags(file string, src []byte) []diag.Diagnostic {
	sh, _, markup := shapeBoundsFile(filepath.Base(file), src)
	if sh == nil || sh.Type == "" || markup < 0 || markup > len(src) {
		return nil
	}
	var attrs, class *transpile.ShapeField
	for i, f := range sh.Fields {
		switch {
		case f.Role != transpile.Prop:
		case f.Name == "Attrs" && strings.Contains(f.Type, "Attr"):
			attrs = &sh.Fields[i]
		case f.Name == "Class" && f.Type == "string":
			class = &sh.Fields[i]
		}
	}
	field := func(name string) string {
		if sh.Recv != "" {
			return sh.Recv + "." + name
		}
		return name
	}
	var out []diag.Diagnostic
	if attrs != nil {
		type tag struct {
			name          string
			attrs, events bool
			eventNames    []string
		}
		var tags []tag
		for i := markup; i < len(src); i++ {
			if src[i] != '<' || i+1 >= len(src) || !isLetter(src[i+1]) {
				continue
			}
			t, ok := readTag(src, i)
			if !ok || t.close || !t.done {
				continue
			}
			tg := tag{name: t.name}
			for _, a := range t.attrs {
				if isEventAttr(a.name) {
					tg.events, tg.eventNames = true, append(tg.eventNames, a.name)
				}
			}
			for _, m := range tagSpread.FindAllSubmatch(src[t.nameEnd:t.end], -1) {
				switch s := string(m[1]); {
				case s == field("Attrs") || s == "Attrs":
					tg.attrs = true
				case eventSpred.MatchString(s):
					tg.events, tg.eventNames = true, append(tg.eventNames, "{"+s+"...}")
				}
			}
			tags = append(tags, tg)
			i = t.end - 1
		}
		target, spreadOn := -1, -1
		for i, t := range tags {
			if t.events && target < 0 {
				target = i
			}
			if t.attrs && spreadOn < 0 {
				spreadOn = i
			}
		}
		why := "its root"
		if target < 0 && len(tags) > 0 {
			target = 0
		} else if target >= 0 {
			why = "the element its events (" + strings.Join(tags[target].eventNames, ", ") + ") are on"
		}
		at := diag.Span{File: file, Line: attrs.Line}
		switch {
		case spreadOn < 0:
			out = append(out, *diag.Warn("W0602", "%s declares Attrs and spreads them on no element: the attributes a caller writes that %s doesn't declare (onClick, id, aria-*) are lost", sh.Type, sh.Type).
				At(at).WithHelp("spread them on %s: <%s … {%s...}>", why, tags[max(target, 0)].name, field("Attrs")))
		case target >= 0 && spreadOn != target:
			out = append(out, *diag.Warn("W0602", "%s spreads Attrs on <%s>, not on <%s>, %s: a caller's onClick lands on the wrong element", sh.Type, tags[spreadOn].name, tags[target].name, why).
				At(at).WithHelp("spread {%s...} on <%s>", field("Attrs"), tags[target].name))
		}
	}
	if class != nil {
		names := regexp.QuoteMeta(field("Class"))
		if sh.Recv != "" {
			names = "(?:" + names + "|Class)"
		}
		if !regexp.MustCompile(fmt.Sprintf(classLast.String(), names)).Match(src) {
			out = append(out, *diag.Warn("W0602", "%s declares Class and doesn't merge it last: a caller's classes don't win over the component's", sh.Type).
				At(diag.Span{File: file, Line: class.Line}).WithHelp(`pass it last to the merge: class={cn(base, %s)}, or a ui.Variants' Class(…, "class", %s)`, field("Class"), field("Class")))
		}
	}
	return out
}

// vetForwarding is W0602 of the app's vendored components: the .vuka.html
// files of [ui]'s components directory.
func vetForwarding(root string) []diag.Diagnostic {
	cfg, ok, err := readUIConfig(root)
	if err != nil || !ok {
		return nil
	}
	var out []diag.Diagnostic
	filepath.WalkDir(filepath.Join(root, filepath.FromSlash(cfg.Dir)), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".vuka.html") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil || !bytes.Contains(src, []byte("<script")) && !bytes.Contains(src, []byte("type ")) {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		out = append(out, forwardingDiags(filepath.ToSlash(rel), src)...)
		return nil
	})
	return out
}
