package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// vuka ui upgrade: a library's components the app has ([ui.sources]) taken
// to the version vuka ui add takes now, each file merged three ways — the
// library's at the version recorded (base), at the new one (theirs), and
// the app's (ours) — so the app's edits stay.

// upgradeDir is a directory of the components directory being upgraded.
type upgradeDir struct {
	name     string // its directory in the components directory
	from, to *libSource
	files    []upgradeFile
	// pending is an upgrade to the same version that left conflicts
	// ([ui.upgrading]): the app's files are its resolution, kept as they
	// are, once they have no markers.
	pending bool
}

// markers are the files of the directory with conflict markers left.
func (d *upgradeDir) markers() []string {
	var out []string
	for _, uf := range d.files {
		if conflictMarker.Match(uf.ours) {
			out = append(out, uf.name)
		}
	}
	return out
}

var conflictMarker = regexp.MustCompile(`(?m)^(?:<<<<<<< yours|>>>>>>> \S+@\S+)$`)

// upgradeFile is one of its files, the three sides of the merge (nil where
// a side hasn't the file).
type upgradeFile struct {
	name               string
	base, theirs, ours []byte
}

// uiUpgrade is vuka ui upgrade.
func uiUpgrade(root, modPath string, f *uiFlags, w io.Writer) error {
	cfg, ok, err := readUIConfig(root)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("vuka ui upgrade: vuka.toml has no [ui]: run vuka ui init first")
	}
	lc, err := readUILib(root)
	if err != nil {
		return err
	}
	names, err := upgradeNames(lc, cfg, f.pos)
	if err != nil {
		return err
	}
	appImport := modPath + "/" + cfg.Dir
	opened := map[string]*libSource{}
	open := func(lib, version string) (*libSource, error) {
		spec, err := lc.library(lib)
		if err != nil {
			return nil, err
		}
		if version == "" {
			version = spec.Version
		}
		key := lib + "@" + version
		if s := opened[key]; s != nil {
			return s, nil
		}
		s, err := openLibrary(root, spec, version)
		if err == nil {
			opened[key] = s
		}
		return s, err
	}
	var dirs []*upgradeDir
	var fromDir []string
	for _, n := range names {
		source := lc.sources[n]
		lib, version := libOf(source), versionOf(source)
		spec, err := lc.library(lib)
		if err != nil {
			return err
		}
		if version == "" || spec.Dir != "" {
			fromDir = append(fromDir, lib+"/"+n)
			continue
		}
		if version == spec.Version {
			continue
		}
		from, err := open(lib, version)
		if err != nil {
			return err
		}
		to, err := open(lib, "")
		if err != nil {
			return err
		}
		d := &upgradeDir{name: n, from: from, to: to, pending: lc.upgrading[n] == to.label()}
		if err := d.read(root, cfg, appImport); err != nil {
			return err
		}
		dirs = append(dirs, d)
	}
	if len(fromDir) > 0 {
		fmt.Fprintf(w, "%s: read from a directory, with no version to merge from (vuka ui diff shows how they differ)\n", strings.Join(fromDir, ", "))
	}
	if f.check {
		return upgradeCheck(dirs, len(names)-len(fromDir), w)
	}
	if len(dirs) == 0 {
		if len(names) > len(fromDir) {
			fmt.Fprintln(w, "the app's library components are up to date")
		}
		return nil
	}
	out := &uiWriter{root: root, dryRun: f.dryRun}
	var conflicted []string
	upgraded := map[string]*libSource{}
	for _, d := range dirs {
		label := libOf(d.to.label()) + "/" + d.name
		if d.pending {
			if m := d.markers(); len(m) > 0 {
				out.note("%s: %s still %s conflict markers", label, strings.Join(m, ", "), map[bool]string{true: "has", false: "have"}[len(m) == 1])
				conflicted = append(conflicted, label)
				continue
			}
			out.note("%s: its conflicts are resolved", label)
			lc.sources[d.name] = d.to.label()
			delete(lc.upgrading, d.name)
			upgraded[libOf(d.to.label())] = d.to
			continue
		}
		clean := true
		for _, uf := range d.files {
			rel := path.Join(cfg.Dir, d.name, uf.name)
			switch merged, conflicts := merge3(uf.base, uf.ours, uf.theirs, d.to.label()); {
			case uf.theirs == nil && uf.ours != nil && bytes.Equal(uf.ours, uf.base):
				out.remove(rel, d.to.label()+" hasn't it")
			case uf.theirs == nil:
				if uf.ours != nil {
					out.note("%s is yours: %s hasn't it", rel, d.to.label())
				}
			case uf.ours == nil && uf.base != nil:
				out.note("%s: the app removed it, %s has it (vuka ui diff %s/%s shows it)", rel, d.to.label(), libOf(d.to.label()), d.name)
			case uf.ours == nil:
				out.write(rel, uf.theirs, "new in "+d.to.label())
			case bytes.Equal(merged, uf.ours):
			case conflicts > 0:
				what := "1 conflict to resolve"
				if conflicts > 1 {
					what = fmt.Sprintf("%d conflicts to resolve", conflicts)
				}
				out.write(rel, merged, what)
				clean = false
			case bytes.Equal(uf.ours, uf.base):
				out.write(rel, merged, d.to.label())
			default:
				out.write(rel, merged, d.to.label()+", merged with the app's changes")
			}
		}
		if clean {
			lc.sources[d.name] = d.to.label()
			delete(lc.upgrading, d.name)
			upgraded[libOf(d.to.label())] = d.to
		} else {
			lc.upgrading[d.name] = d.to.label()
			conflicted = append(conflicted, label)
		}
	}
	gets := map[string]string{}
	for _, lib := range slices.Sorted(mapKeys(upgraded)) {
		src := upgraded[lib]
		if err := upgradeNew(out, cfg, lc, src, appImport, gets); err != nil {
			return err
		}
		if err := libTheme(out, cfg, src, lc, false); err != nil {
			return err
		}
	}
	toml, err := out.read("vuka.toml")
	if err != nil {
		return err
	}
	next := setTOMLTable(toml, "ui.sources", lc.sourcesTable())
	switch {
	case len(lc.upgrading) > 0:
		next = setTOMLTable(next, "ui.upgrading", sourcesText("ui.upgrading", lc.upgrading))
	case tomlHasTable(next, "ui.upgrading"):
		next = removeTOMLTable(next, "ui.upgrading")
	}
	if !bytes.Equal(next, toml) {
		out.write("vuka.toml", next, "[ui.sources]")
	}
	if err := out.commit(w); err != nil {
		return err
	}
	if !f.dryRun {
		if err := getModules(root, gets, w); err != nil {
			return err
		}
	}
	if len(conflicted) > 0 {
		return fmt.Errorf("vuka ui upgrade: %s %s conflicts (marked <<<<<<< ======= >>>>>>> in the files above): resolve them, then run vuka ui upgrade again, which records the new version", strings.Join(conflicted, ", "), map[bool]string{true: "has", false: "have"}[len(conflicted) == 1])
	}
	return nil
}

// tomlHasTable is whether toml has the table name.
func tomlHasTable(toml []byte, name string) bool {
	for _, l := range strings.Split(string(toml), "\n") {
		if t := strings.TrimSpace(stripComment(l)); strings.HasPrefix(t, "[") && strings.TrimSpace(strings.Trim(t, "[]")) == name {
			return true
		}
	}
	return false
}

// removeTOMLTable is toml without the table name.
func removeTOMLTable(toml []byte, name string) []byte {
	s := string(setTOMLTable(toml, name, nil))
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return []byte(strings.TrimRight(s, "\n") + "\n")
}

// upgradeNames are the directories of [ui.sources] the arguments name
// (library/name, or a directory's name), all of them when none do.
func upgradeNames(lc *uiLibConfig, cfg uiConfig, args []string) ([]string, error) {
	all := slices.Sorted(mapKeys(lc.sources))
	if len(args) == 0 {
		if len(all) == 0 {
			return nil, fmt.Errorf("vuka ui upgrade: %s has no library components (vuka.toml's [ui.sources] is empty); vuka ui diff compares vuka's own with the registry's", cfg.Dir)
		}
		return all, nil
	}
	var labels []string
	for _, n := range all {
		labels = append(labels, libOf(lc.sources[n])+"/"+n)
	}
	var out []string
	for _, a := range args {
		lib, name, qualified := strings.Cut(strings.ToLower(a), "/")
		if !qualified {
			name = lib
		}
		s, ok := lc.sources[name]
		switch {
		case ok && (!qualified || libOf(s) == lib):
			if !slices.Contains(out, name) {
				out = append(out, name)
			}
		case len(all) == 0:
			return nil, fmt.Errorf("vuka ui upgrade: %s has no library components (vuka.toml's [ui.sources] is empty)", cfg.Dir)
		default:
			return nil, fmt.Errorf("vuka ui upgrade: %s came from no library%s; the app's library components are %s (vuka's own components: vuka ui diff, then vuka ui add -f)",
				a, suggest(strings.ToLower(a), append(slices.Clone(labels), all...), ""), strings.Join(labels, ", "))
		}
	}
	return out, nil
}

// read reads the three sides of each file of the directory.
func (d *upgradeDir) read(root string, cfg uiConfig, appImport string) error {
	side := func(s *libSource) (map[string][]byte, error) {
		dir := s.pkgDir(d.name)
		files, err := pkgFiles(dir)
		if err != nil {
			return nil, fmt.Errorf("vuka ui upgrade: %s hasn't %s", s.label(), d.name)
		}
		out := map[string][]byte{}
		for _, f := range files {
			if out[f], err = s.appFileOf(dir, f, appImport); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	base, err := side(d.from)
	if err != nil {
		return err
	}
	theirs, err := side(d.to)
	if err != nil {
		return err
	}
	names := slices.Sorted(mapKeys(base))
	for n := range theirs {
		if base[n] == nil {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	for _, n := range names {
		ours, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(cfg.Dir), d.name, n))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		d.files = append(d.files, upgradeFile{name: n, base: base[n], theirs: theirs[n], ours: ours})
	}
	return nil
}

// upgradeCheck is vuka ui upgrade --check: which components have changes
// upstream and which of their files the app edited, failing when there
// are changes to take.
func upgradeCheck(dirs []*upgradeDir, current int, w io.Writer) error {
	var pending []string
	for _, d := range dirs {
		var changed, edited []string
		for _, uf := range d.files {
			if !bytes.Equal(uf.base, uf.theirs) {
				changed = append(changed, uf.name)
			}
			if uf.base != nil && !bytes.Equal(uf.base, uf.ours) {
				edited = append(edited, uf.name)
			}
		}
		label := libOf(d.to.label()) + "/" + d.name
		line := fmt.Sprintf("%s: %s → %s", label, d.from.version, d.to.version)
		if d.pending {
			if m := d.markers(); len(m) > 0 {
				line += ", conflicts to resolve in " + strings.Join(m, ", ")
			} else {
				line += ", its conflicts resolved"
			}
			fmt.Fprintln(w, line)
			pending = append(pending, label)
			continue
		}
		if len(changed) == 0 {
			line += ", no changes upstream"
		} else {
			line += ", changed upstream: " + strings.Join(changed, ", ")
			pending = append(pending, label)
		}
		if len(edited) > 0 {
			line += "; edited in the app: " + strings.Join(edited, ", ")
		}
		fmt.Fprintln(w, line)
	}
	if current > 0 && len(dirs) == 0 {
		fmt.Fprintln(w, "the app's library components are up to date")
	}
	if len(pending) > 0 {
		return fmt.Errorf("vuka ui upgrade --check: %s %s changes upstream (vuka ui upgrade takes them)", strings.Join(pending, ", "), map[bool]string{true: "has", false: "have"}[len(pending) == 1])
	}
	return nil
}

// upgradeNew queues the packages of the library that the upgraded
// components need at its new version and the app hasn't, and adds the
// modules they import to gets.
func upgradeNew(out *uiWriter, cfg uiConfig, lc *uiLibConfig, src *libSource, appImport string, gets map[string]string) error {
	lib := src.spec.Name
	var comps []string
	for _, c := range src.components() {
		if s, ok := lc.sources[c]; ok && s == src.label() {
			comps = append(comps, c)
		}
	}
	if len(comps) == 0 {
		return nil
	}
	pkgs, ext, err := src.collect(comps)
	if err != nil {
		return err
	}
	for _, p := range pkgs {
		if s, ok := lc.sources[p.name]; ok {
			if libOf(s) != lib {
				return fmt.Errorf("vuka ui upgrade: %s/%s is %s's (vuka.toml's [ui.sources]), not %s's", cfg.Dir, p.name, libOf(s), lib)
			}
			continue
		}
		for _, f := range p.files {
			b, err := src.appFileOf(p.dir, f, appImport)
			if err != nil {
				return err
			}
			rel := path.Join(cfg.Dir, p.name, f)
			if have, err := out.read(rel); err != nil {
				return err
			} else if have == nil {
				out.write(rel, b, "needed at "+src.label())
			}
		}
		lc.sources[p.name] = src.label()
	}
	for m, v := range ext {
		gets[m] = v
		if v, ok := toolchainModules()[m]; ok {
			gets[m] = v
		}
	}
	return nil
}

// pkgDir is the directory of the source the app has as name: a component's,
// else a package outside the components directory (utils).
func (s *libSource) pkgDir(name string) string {
	dir := filepath.Join(s.dir, filepath.FromSlash(s.spec.Components), name)
	if _, err := os.Stat(dir); err != nil {
		return findPkgDir(s, name)
	}
	return dir
}

// merge3 merges the changes from base to theirs into ours, line by line: a
// change only one side made is taken, the same change on both once, and
// changes both made differently are a conflict between markers. A nil base
// is a file neither had.
func merge3(base, ours, theirs []byte, theirsLabel string) ([]byte, int) {
	bl, ol, tl := splitLines(base), splitLines(ours), splitLines(theirs)
	ids := map[string]int{}
	intern := func(lines []string) []int {
		out := make([]int, len(lines))
		for i, l := range lines {
			id, ok := ids[l]
			if !ok {
				id = len(ids)
				ids[l] = id
			}
			out[i] = id
		}
		return out
	}
	bi, oi, ti := intern(bl), intern(ol), intern(tl)
	match := func(other []int) []int {
		m := make([]int, len(bi))
		for i := range m {
			m[i] = -1
		}
		pairs, _ := myers(bi, other, len(bi)+len(other))
		for _, p := range pairs {
			m[p[0]] = p[1]
		}
		return m
	}
	mo, mt := match(oi), match(ti)
	var buf bytes.Buffer
	conflicts := 0
	same := func(a, b []string) bool { return slices.Equal(a, b) }
	emit := func(lines []string) {
		for _, l := range lines {
			buf.WriteString(l)
		}
	}
	marker := func(m string) {
		if buf.Len() > 0 && !bytes.HasSuffix(buf.Bytes(), []byte("\n")) {
			buf.WriteByte('\n')
		}
		buf.WriteString(m + "\n")
	}
	b, o, t := 0, 0, 0
	for b < len(bl) || o < len(ol) || t < len(tl) {
		if b < len(bl) && mo[b] == o && mt[b] == t {
			buf.WriteString(bl[b])
			b, o, t = b+1, o+1, t+1
			continue
		}
		nb, no, nt := len(bl), len(ol), len(tl)
		for j := b; j < len(bl); j++ {
			if mo[j] >= o && mt[j] >= t {
				nb, no, nt = j, mo[j], mt[j]
				break
			}
		}
		cb, co, ct := bl[b:nb], ol[o:no], tl[t:nt]
		switch {
		case same(co, cb):
			emit(ct)
		case same(ct, cb), same(co, ct):
			emit(co)
		default:
			conflicts++
			marker("<<<<<<< yours")
			emit(co)
			marker("=======")
			emit(ct)
			marker(">>>>>>> " + theirsLabel)
		}
		b, o, t = nb, no, nt
	}
	return buf.Bytes(), conflicts
}
