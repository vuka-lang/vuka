package main

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"time"
)

// The registry vuka ui reads: index.json and the files it names, served
// over HTTPS or kept in a directory. Vuka's own is cmd/vuka/registry in its
// repository, fetched at the commit this vuka was built from, so the
// components match the libraries the app builds against; a copy is built
// in for --offline and for when the fetch fails.

//go:embed registry
var registryFS embed.FS

// registrySchema is the index format this vuka reads.
const registrySchema = 1

// registry is a registry's index.
type registry struct {
	Schema  int                     `json:"schema"`
	Name    string                  `json:"name"`
	Package string                  `json:"package"` // the package clause the files are written with
	Styles  map[string]registryFile `json:"styles"`  // a style's name → its tokens' stylesheet
	Items   []registryItem          `json:"items"`

	src   regSource
	note  string            // where it came from, said first
	files map[string][]byte // the files read, checked against the index
}

// registryItem is a component: its files (paths in the registry, written
// under the components directory without their first element), the items
// it uses, the versions of vuka's libraries it needs, and its tier — 0 a
// helper, 1 HTML and JS commands in the browser, 2 state on the server
// over the live connection, 3 an island.
type registryItem struct {
	Name        string            `json:"name"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Tier        int               `json:"tier"`
	Files       []registryFile    `json:"files"`
	Deps        []string          `json:"deps,omitempty"`
	Requires    map[string]string `json:"requires,omitempty"` // vuka, ui or web → its lowest version
}

type registryFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// requireModules are the modules an item's requires names.
var requireModules = map[string]string{"vuka": vukaModPath, "ui": uiModPath, "web": webModPath}

// regSource is where a registry is read from: an HTTPS base URL, a
// directory, or the copy built in (both empty).
type regSource struct {
	url, dir string
	fallback bool // a fetch that fails falls back to the copy built in
}

func (s regSource) String() string {
	switch {
	case s.url != "":
		return s.url
	case s.dir != "":
		return s.dir
	}
	return "the registry built into vuka"
}

func (s regSource) where(p string) string {
	switch {
	case s.url != "":
		return s.url + "/" + p
	case s.dir != "":
		return filepath.Join(s.dir, filepath.FromSlash(p))
	}
	return "registry/" + p
}

// fetchError is a registry file that couldn't be fetched.
type fetchError struct{ err error }

func (e *fetchError) Error() string { return e.err.Error() }
func (e *fetchError) Unwrap() error { return e.err }

// registryHTTP fetches registries; registryMax caps a file's size.
var (
	registryHTTP = &http.Client{Timeout: 30 * time.Second}
	registryMax  = int64(4 << 20)
)

func (s regSource) read(p string) ([]byte, error) {
	switch {
	case s.url != "":
		u := s.where(p)
		resp, err := registryHTTP.Get(u)
		if err != nil {
			return nil, &fetchError{err}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, &fetchError{fmt.Errorf("GET %s: %s", u, resp.Status)}
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, registryMax+1))
		if err != nil {
			return nil, &fetchError{fmt.Errorf("GET %s: %v", u, err)}
		}
		if int64(len(b)) > registryMax {
			return nil, fmt.Errorf("GET %s: larger than %d bytes", u, registryMax)
		}
		return b, nil
	case s.dir != "":
		return os.ReadFile(s.where(p))
	}
	return registryFS.ReadFile("registry/" + p)
}

// defaultRegistry is vuka's registry at a commit or tag; registryRef is the
// one this vuka was built from, "" for a build of a checkout, which uses
// its own copy.
var (
	defaultRegistry = "https://raw.githubusercontent.com/vuka-lang/vuka/%s/cmd/vuka/registry"
	registryRef     = func() string {
		bi, ok := debug.ReadBuildInfo()
		if !ok {
			return ""
		}
		return refOf(bi.Main.Version)
	}
)

var pseudoRev = regexp.MustCompile(`^v\d+\.\d+\.\d+-(?:[0-9A-Za-z.-]*\.)?\d{14}-([0-9a-f]{12})$`)

// refOf is the git ref a module version was made from: a release's tag, a
// pseudo-version's commit; "" for a local build.
func refOf(v string) string {
	switch {
	case v == "" || v == "(devel)" || strings.Contains(v, "+"):
		return ""
	case pseudoRev.MatchString(v):
		return pseudoRev.FindStringSubmatch(v)[1]
	}
	return v
}

// registrySource is the registry a command reads: --registry, else
// [ui] registry, else vuka's own (built in with --offline).
func registrySource(f *uiFlags, cfg uiConfig, root string) (regSource, error) {
	switch {
	case f.registry != "" && f.offline:
		return regSource{}, errors.New("vuka ui: --offline reads the registry built into vuka; it takes no --registry")
	case f.registry != "":
		return parseRegistry(f.registry, "")
	case cfg.Registry != "" && !f.offline:
		return parseRegistry(cfg.Registry, root)
	case f.offline:
		return regSource{}, nil
	}
	ref := registryRef()
	if ref == "" {
		return regSource{}, nil
	}
	return regSource{url: fmt.Sprintf(defaultRegistry, ref), fallback: true}, nil
}

var urlScheme = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`)

// parseRegistry is the registry s names: an https URL, or a directory
// (relative to base).
func parseRegistry(s, base string) (regSource, error) {
	switch {
	case strings.HasPrefix(s, "https://"):
		return regSource{url: strings.TrimRight(s, "/")}, nil
	case urlScheme.MatchString(s):
		return regSource{}, fmt.Errorf("vuka ui: the registry %s: an https:// URL or a directory", s)
	}
	dir := filepath.FromSlash(s)
	if !filepath.IsAbs(dir) && base != "" {
		dir = filepath.Join(base, dir)
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return regSource{}, err
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return regSource{}, fmt.Errorf("vuka ui: the registry %s: no such directory", s)
	}
	return regSource{dir: dir}, nil
}

// openRegistry reads the index of the registry a command uses.
func openRegistry(f *uiFlags, cfg uiConfig, root string) (*registry, error) {
	src, err := registrySource(f, cfg, root)
	if err != nil {
		return nil, err
	}
	r, err := readRegistry(src)
	var fe *fetchError
	if errors.As(err, &fe) && src.fallback {
		return builtIn(err)
	}
	return r, err
}

// builtIn is the registry built into vuka, used because of err.
func builtIn(err error) (*registry, error) {
	r, rerr := readRegistry(regSource{})
	if rerr != nil {
		return nil, rerr
	}
	r.note = fmt.Sprintf("couldn't fetch the registry (%v): used the copy built into vuka", err)
	return r, nil
}

func readRegistry(src regSource) (*registry, error) {
	b, err := src.read("index.json")
	if err != nil {
		var fe *fetchError
		if errors.As(err, &fe) {
			return nil, err
		}
		return nil, fmt.Errorf("vuka ui: the registry's index: %v", err)
	}
	r := &registry{src: src, files: map[string][]byte{}}
	if err := json.Unmarshal(b, r); err != nil {
		return nil, fmt.Errorf("vuka ui: %s: %v", src.where("index.json"), err)
	}
	if err := r.check(); err != nil {
		return nil, fmt.Errorf("vuka ui: %s: %v", src.where("index.json"), err)
	}
	if src.url != "" || src.dir != "" {
		r.note = "registry: " + src.String()
	}
	return r, nil
}

var (
	itemName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	sha256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)
	semverRe = regexp.MustCompile(`^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$`)
)

// check is whether the index is one this vuka reads, and sound.
func (r *registry) check() error {
	switch {
	case r.Schema == 0:
		return errors.New(`no "schema": an index of schema 1 says "schema": 1`)
	case r.Schema > registrySchema:
		return fmt.Errorf("schema %d, newer than this vuka reads (%d): vuka upgrade", r.Schema, registrySchema)
	case r.Schema != registrySchema:
		return fmt.Errorf("schema %d: this vuka reads schema %d", r.Schema, registrySchema)
	case !goPackageName.MatchString(r.Package):
		return fmt.Errorf("package %q isn't a package name", r.Package)
	}
	for name, s := range r.Styles {
		if err := checkFile(s, false); err != nil {
			return fmt.Errorf("style %s: %v", name, err)
		}
	}
	seen := map[string]bool{}
	for _, it := range r.Items {
		if !itemName.MatchString(it.Name) || seen[it.Name] {
			return fmt.Errorf("item %q: a name in lower case, once", it.Name)
		}
		seen[it.Name] = true
		if it.Tier < 0 || it.Tier > 3 || len(it.Files) == 0 {
			return fmt.Errorf("item %s: a tier from 0 to 3 and its files", it.Name)
		}
		for _, f := range it.Files {
			if err := checkFile(f, true); err != nil {
				return fmt.Errorf("item %s: %v", it.Name, err)
			}
		}
		for lib, v := range it.Requires {
			if requireModules[lib] == "" {
				return fmt.Errorf("item %s requires %s: the libraries are vuka, ui and web", it.Name, lib)
			}
			if !semverRe.MatchString(v) {
				return fmt.Errorf("item %s requires %s %q: a version, vX.Y.Z", it.Name, lib, v)
			}
		}
	}
	return nil
}

// checkFile is whether a file's path stays inside the registry (and, for a
// component's, inside the components directory it is written to) and its
// digest is one.
func checkFile(f registryFile, component bool) error {
	p := f.Path
	bad := p == "" || path.IsAbs(p) || path.Clean(p) != p || strings.ContainsAny(p, `\:`) || !filepath.IsLocal(filepath.FromSlash(p))
	if !bad && component {
		dir, rest, ok := strings.Cut(p, "/")
		bad = !ok || dir == "" || !filepath.IsLocal(filepath.FromSlash(rest))
	}
	switch {
	case bad && component:
		return fmt.Errorf("file %q: a path dir/name, inside the components directory", p)
	case bad:
		return fmt.Errorf("file %q: a relative path inside the registry", p)
	case !sha256Re.MatchString(f.SHA256):
		return fmt.Errorf("file %s: its sha256, 64 hex digits", p)
	}
	return nil
}

// file is a registry file's content, checked against its digest.
func (r *registry) file(f registryFile) ([]byte, error) {
	if b, ok := r.files[f.Path]; ok {
		return b, nil
	}
	b, err := r.src.read(f.Path)
	if err != nil {
		var fe *fetchError
		if errors.As(err, &fe) {
			return nil, err
		}
		return nil, fmt.Errorf("vuka ui: %s: %v", r.src.where(f.Path), err)
	}
	if sum := sha256Hex(b); sum != f.SHA256 {
		return nil, fmt.Errorf("vuka ui: %s: its sha256 is %s, the index says %s: the file isn't the one the index was written for", r.src.where(f.Path), sum, f.SHA256)
	}
	r.files[f.Path] = b
	return b, nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// fetch reads the files items name, falling back to the registry built in
// (all of it, the index too) when vuka's own can't be fetched.
func (r *registry) fetch(items []*registryItem, styles ...string) ([]*registryItem, error) {
	err := r.fetchFiles(items, styles)
	var fe *fetchError
	if errors.As(err, &fe) && r.src.fallback {
		b, berr := builtIn(err)
		if berr != nil {
			return nil, berr
		}
		*r = *b
		names := make([]string, len(items))
		for i, it := range items {
			names[i] = it.Name
		}
		if items, err = r.resolve("add", names); err != nil {
			return nil, err
		}
		err = r.fetchFiles(items, styles)
	}
	return items, err
}

func (r *registry) fetchFiles(items []*registryItem, styles []string) error {
	for _, s := range styles {
		if f, ok := r.Styles[s]; ok {
			if _, err := r.file(f); err != nil {
				return err
			}
		}
	}
	for _, it := range items {
		for _, f := range it.Files {
			if _, err := r.file(f); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *registry) item(name string) *registryItem {
	for i := range r.Items {
		if r.Items[i].Name == name {
			return &r.Items[i]
		}
	}
	return nil
}

// components are the names of the items vuka ui add takes, the helpers
// left out.
func (r *registry) components() []string {
	var out []string
	for _, it := range r.Items {
		if it.Tier > 0 {
			out = append(out, it.Name)
		}
	}
	slices.Sort(out)
	return out
}

func (r *registry) list(w io.Writer) {
	if r.note != "" {
		fmt.Fprintln(w, r.note)
	}
	tiers := []string{"", "browser", "live", "island"}
	for _, n := range r.components() {
		it := r.item(n)
		fmt.Fprintf(w, "%-8s %-8s %s\n", it.Name, tiers[it.Tier], it.Description)
	}
}

// resolve is the items names asks for and those they use, each after the
// ones it uses, in the order they are named.
func (r *registry) resolve(cmd string, names []string) ([]*registryItem, error) {
	var out []*registryItem
	state := map[string]int{} // 1 being visited, 2 done
	var visit func(name string, path []string) error
	visit = func(name string, path []string) error {
		switch state[name] {
		case 2:
			return nil
		case 1:
			return fmt.Errorf("vuka ui: the registry's items use each other in a circle: %s", strings.Join(append(path, name), " → "))
		}
		it := r.item(name)
		if it == nil {
			if len(path) > 0 {
				return fmt.Errorf("vuka ui: the registry's %s uses %s, which it hasn't", path[len(path)-1], name)
			}
			return fmt.Errorf("vuka ui %s: no component %q%s; the components are %s", cmd, name,
				suggest(name, r.components(), ""), strings.Join(r.components(), ", "))
		}
		state[name] = 1
		for _, d := range it.Deps {
			if err := visit(d, append(path, name)); err != nil {
				return err
			}
		}
		state[name] = 2
		out = append(out, it)
		return nil
	}
	for _, n := range names {
		if err := visit(strings.ToLower(n), nil); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// checkRequires is whether the app's go.mod requires the versions of
// vuka's libraries the items need, saying which go get to run when not. A
// module the app replaces is taken as it is.
func checkRequires(root string, items []*registryItem) error {
	if !slices.ContainsFunc(items, func(it *registryItem) bool { return len(it.Requires) > 0 }) {
		return nil
	}
	m, err := readGoMod(root)
	if err != nil {
		return err
	}
	need := map[string]string{} // module → the version to get
	who := map[string][]string{}
	for _, it := range items {
		for _, lib := range slices.Sorted(mapKeys(it.Requires)) {
			p, v := requireModules[lib], it.Requires[lib]
			if have := m.requires(p); m.replaced(p) != "" || have != "" && compareVersions(have, v) >= 0 {
				continue
			}
			if have, ok := need[p]; !ok || compareVersions(have, v) < 0 {
				need[p] = v
			}
			who[p] = append(who[p], it.Name)
		}
	}
	if len(need) == 0 {
		return nil
	}
	var lines, gets []string
	for _, p := range slices.Sorted(mapKeys(need)) {
		had := "the app doesn't require it"
		if have := m.requires(p); have != "" {
			had = "the app requires " + have
		}
		verb := "needs"
		if len(who[p]) > 1 {
			verb = "need"
		}
		lines = append(lines, fmt.Sprintf("%s %s %s %s or newer; %s", strings.Join(who[p], ", "), verb, p, need[p], had))
		gets = append(gets, p+"@"+need[p])
	}
	return fmt.Errorf("vuka ui add: %s. Upgrade first, then add again:\n\n\tgo get %s", strings.Join(lines, "; "), strings.Join(gets, " "))
}
