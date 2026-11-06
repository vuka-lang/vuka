package main

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// [ui] icons: a library's components drawn with the app's icons. The
// library's description names its icon package; each of its icons has a
// role (close, check, chevron-down), its name in the package unless the
// description gives it another. The set [ui] icons names maps roles to
// functions of the app's icon package, which take the library's icon props
// (…icon.Props), and vuka ui add writes calls of those in place of the
// library's; a role the set doesn't map keeps the library's icon, with a
// note.

// iconSet is a set of the app's icons, vuka.toml's [ui.icons.<name>].
type iconSet struct {
	name  string
	pkg   string            // its package's import path
	funcs map[string]string // a role → its function in pkg
}

// iconRoles are the roles of the icons whose role isn't their name: the
// x icon is the close button's.
var iconRoles = map[string]string{
	"x": "close", "loader-circle": "spinner", "triangle-alert": "warning", "octagon-x": "error",
	"circle-check": "success", "panel-left": "sidebar", "ellipsis": "more", "ellipsis-vertical": "more-vertical",
}

// iconDecl is an icon of a library's icon package: var Name = Icon("name").
var iconDecl = regexp.MustCompile(`(?m)^\s*(?:var\s+)?([A-Z]\w*)\s*=\s*Icon\("([^"]+)"\)`)

// libIcons are the roles of the library's icons, by their names in its
// icon package; nil when it has none.
func (s *libSource) libIcons() map[string]string {
	if s.spec.Icons == "" {
		return nil
	}
	if s.iconRoles != nil {
		return s.iconRoles
	}
	s.iconRoles = map[string]string{}
	dir := filepath.Join(s.dir, filepath.FromSlash(s.spec.Components), s.spec.Icons)
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil || strings.HasSuffix(f, "_test.go") {
			continue
		}
		for _, m := range iconDecl.FindAllSubmatch(b, -1) {
			role := string(m[2])
			if r, ok := iconRoles[role]; ok {
				role = r
			}
			s.iconRoles[string(m[1])] = role
		}
	}
	return s.iconRoles
}

// readIconSet is the set of the app's icons [ui] icons names for the
// library, nil when it names none or the library's own.
func readIconSet(root string, lc *uiLibConfig, spec libSpec) (*iconSet, error) {
	cfg, ok, err := readUIConfig(root)
	if err != nil || !ok || cfg.Icons == "" || cfg.Icons == spec.IconSet || spec.Icons == "" {
		return nil, err
	}
	set, ok := lc.iconSets[cfg.Icons]
	if !ok {
		names := slices.Sorted(mapKeys(lc.iconSets))
		if spec.IconSet != "" {
			names = append(names, spec.IconSet)
		}
		return nil, fmt.Errorf("vuka.toml: [ui] icons = %q: no [ui.icons.%s] maps its icons%s (the sets are %s)", cfg.Icons, cfg.Icons, suggest(cfg.Icons, names, ""), strings.Join(names, ", "))
	}
	if set.pkg == "" {
		return nil, fmt.Errorf("vuka.toml: [ui.icons.%s] names the package of its icons (package = \"example.com/shop/components/icons\")", cfg.Icons)
	}
	if first, _, _ := strings.Cut(set.pkg, "/"); !strings.Contains(first, ".") {
		if _, modPath, err := load.ModuleRoot(root); err == nil {
			set.pkg = modPath + "/" + set.pkg
		}
	}
	return set, nil
}

// withIcons is a .go or .templ file of a library's package with the
// calls of the library's icons replaced by the set's for their roles,
// importing the set's package, and the library's icon package no longer
// when nothing else uses it. A role the set hasn't is noted.
func (s *libSource) withIcons(src []byte, iconImport string) []byte {
	roles := s.libIcons()
	if s.icons == nil || len(roles) == 0 {
		return src
	}
	local := importedAs(src, iconImport)
	if local == "" {
		return src
	}
	setName := path.Base(s.icons.pkg)
	if setName == local {
		setName = "app" + setName
	}
	use := regexp.MustCompile(`\b` + regexp.QuoteMeta(local) + `\.([A-Z]\w*)\b`)
	changed := false
	out := use.ReplaceAllFunc(src, func(m []byte) []byte {
		name := string(m[len(local)+1:])
		role, isIcon := roles[name]
		if !isIcon {
			return m
		}
		fn, mapped := s.icons.funcs[role]
		if !mapped {
			s.note(fmt.Sprintf("the icon set %s has no %s icon: kept %s's (%s.%s)", s.icons.name, role, s.spec.Name, local, name))
			return m
		}
		changed = true
		return []byte(setName + "." + fn)
	})
	if !changed {
		return src
	}
	spec := strconv.Quote(s.icons.pkg)
	if setName != path.Base(s.icons.pkg) {
		spec = setName + " " + spec
	}
	out = addImportSpec(out, spec)
	if !use.Match(out) {
		out = removeImport(out, iconImport)
	}
	return out
}

func (s *libSource) note(n string) {
	if !slices.Contains(s.notes, n) {
		s.notes = append(s.notes, n)
	}
}

// addImportSpec adds an import spec ("path", or name "path") to a Go or
// templ file's imports.
func addImportSpec(src []byte, spec string) []byte {
	if bytes.Contains(src, []byte(spec)) {
		return src
	}
	if loc := regexp.MustCompile(`(?m)^import\s*\(\n`).FindIndex(src); loc != nil {
		return slices.Concat(src[:loc[1]], []byte("\t"+spec+"\n"), src[loc[1]:])
	}
	if loc := regexp.MustCompile(`(?m)^import\s+[^\n]*\n`).FindIndex(src); loc != nil {
		return slices.Concat(src[:loc[1]], []byte("import "+spec+"\n"), src[loc[1]:])
	}
	if loc := regexp.MustCompile(`(?m)^package\s+\w+[^\n]*\n`).FindIndex(src); loc != nil {
		return slices.Concat(src[:loc[1]], []byte("\nimport "+spec+"\n"), src[loc[1]:])
	}
	return src
}

// removeImport removes a file's import of importPath.
func removeImport(src []byte, importPath string) []byte {
	q := regexp.QuoteMeta(strconv.Quote(importPath))
	if re := regexp.MustCompile(`(?m)^import\s+(?:\w+\s+)?` + q + `[^\n]*\n`); re.Match(src) {
		return re.ReplaceAll(src, nil)
	}
	return regexp.MustCompile(`(?m)^\s*(?:\w+\s+)?`+q+`[^\n]*\n`).ReplaceAll(src, nil)
}
