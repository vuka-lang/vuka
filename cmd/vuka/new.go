package main

import (
	"bytes"
	"crypto/rand"
	"embed"
	"errors"
	"flag"
	"fmt"
	goformat "go/format"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"text/template"
	"unicode"

	"github.com/vuka-lang/vuka/internal/format"
)

// The orm a new app requires beside the toolchain set (toolchain.go); vuka
// mod tidy brings in the rest. The scaffolds are written against it: CI's
// cmd job points new apps at orm main (VUKA_NEW_REPLACE), so a scaffold
// out of step with the orm fails there, not in the next release.
const ormVersion = "v0.9.2-0.20261020045121-493937f0b6bd"

// scaffoldFS holds the files vuka new and vuka add write. A path segment
// name@tag1+tag2 is written as name when every tag holds (scaffold.tags);
// a .tmpl file is a text/template delimited {% %}, and one rendering to
// nothing but blanks isn't written.
//
//go:embed all:scaffold
var scaffoldFS embed.FS

// scaffold is what vuka new makes: the answers to its questions and what
// follows from them.
type scaffold struct {
	Dir      string
	Module   string
	Name     string // the app's name as people read it (APP_NAME)
	UI       string // live, templ, api
	DB       string // sqlite, postgres, mysql, none
	Mail     bool
	Auth     bool
	Frontend string // none, react, vue, svelte
	Islands  bool   // a sample Vue island (needs live pages and its own web/ root)

	AdminEmail, AdminPassword string

	VukaVersion, UIVersion, WebVersion, ORMVersion string
	// Replace points modules at local checkouts (VUKA_NEW_REPLACE).
	Replace map[string]string
}

// The questions vuka new asks; each choice's key is its flag's value.
var (
	uiChoices = []choice{
		{"live", "Live pages + Tailwind", "recommended"},
		{"templ", "templ views + Tailwind", ""},
		{"api", "API only", "JSON, no pages"},
	}
	dbChoices = []choice{
		{"sqlite", "SQLite", "a file, no server to run"},
		{"postgres", "PostgreSQL", ""},
		{"mysql", "MySQL", ""},
		{"none", "None", ""},
	}
	mailChoices = []choice{
		{"yes", "Log in development, SMTP in production", ""},
		{"no", "None", ""},
	}
	authChoices = []choice{
		{"yes", "Yes, with a login page", "users in the database"},
		{"no", "No", ""},
	}
	frontendChoices = []choice{
		{"none", "None", ""},
		{"react", "React", "a Vite app at /app"},
		{"vue", "Vue", "a Vite app at /app"},
		{"svelte", "Svelte", "a Vite app at /app"},
	}
	islandsChoices = []choice{
		{"no", "No", ""},
		{"yes", "Yes, a sample Vue component", "hydrated in the browser; needs Node.js"},
	}
)

const newUsage = `Usage: vuka new <dir> [module path] [flags]

Start an app: answer a few questions (or give them as flags; --yes takes
the defaults for the rest) and get a project that runs, its tests passing.
Without a terminal, the defaults and flags are used.

Flags:
  --ui=live|templ|api               live pages, templ views, or a JSON API (default live)
  --db=sqlite|postgres|mysql|none   the database (default sqlite)
  --mail=yes|no                     mail, logged in development, SMTP in production (default yes)
  --auth=yes|no                     sign-in, with users in the database (default yes)
  --frontend=none|react|vue|svelte  a Vite app served at /app (default none)
  --islands=yes|no                  a sample Vue island (needs --ui=live --frontend=none) (default no)
  --name=NAME                       the app's name as people read it (default: the directory's)
  --yes, -y                         the defaults for every question no flag answers
  --force                           use a directory that isn't empty; none of its files is overwritten
`

// newArgs is vuka new's command line: the app's directory and module,
// what the flags answer, and whether to ask the rest.
type newArgs struct {
	s     *scaffold
	given map[string]string // ui, db, mail, auth, frontend: a flag's value, else ""
	yes   bool
	force bool
}

func parseNewArgs(args []string) (newArgs, error) {
	fs := flag.NewFlagSet("vuka new", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	given := map[string]*string{}
	for _, k := range []string{"ui", "db", "mail", "auth", "frontend", "islands"} {
		given[k] = fs.String(k, "", "")
	}
	name := fs.String("name", "", "")
	yes := fs.Bool("yes", false, "")
	fs.BoolVar(yes, "y", false, "")
	force := fs.Bool("force", false, "")
	pos, err := interspersed(fs, args)
	if err != nil {
		return newArgs{}, err
	}
	if len(pos) < 1 || len(pos) > 2 {
		return newArgs{}, errors.New("vuka new takes a directory, and a module path after it if you like")
	}
	base := filepath.Base(filepath.Clean(pos[0]))
	a := newArgs{s: &scaffold{Dir: pos[0], Module: base, Name: *name}, given: map[string]string{}, yes: *yes, force: *force}
	if len(pos) == 2 {
		a.s.Module = pos[1]
	}
	if a.s.Name == "" {
		a.s.Name = displayName(base)
	}
	for k, v := range given {
		a.given[k] = *v
	}
	return a, nil
}

// newProject is vuka new: it asks what no flag says, then writes the app.
func newProject(args []string, stdin io.Reader, stdout io.Writer) error {
	a, err := parseNewArgs(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stdout, newUsage)
		return nil
	}
	if err != nil {
		return fmt.Errorf("%v\n\n%s", err, strings.TrimRight(newUsage, "\n"))
	}
	s := a.s
	if err := checkTarget(s.Dir, a.force); err != nil {
		return err
	}
	var p *prompter
	if !a.yes && interactive(stdin, stdout) {
		restore, err := rawTerminal(stdin)
		p = newPrompter(stdin, stdout, err == nil)
		if err == nil {
			defer restore()
		}
	}
	err = s.answer(a.given, p)
	if p != nil {
		p.close()
	}
	if err != nil {
		return err
	}
	if err := s.create(stdout); err != nil {
		return err
	}
	s.summary(stdout)
	return nil
}

// create writes the app and fetches its dependencies.
func (s *scaffold) create(stdout io.Writer) error {
	s.AdminEmail = "admin@example.com"
	s.AdminPassword = newPassword()
	s.VukaVersion, s.UIVersion, s.WebVersion, s.ORMVersion = vukaVersion, uiVersion, webVersion, ormVersion
	if s.Replace == nil {
		s.Replace = replacesFromEnv()
	}
	files, err := s.render()
	if err != nil {
		return err
	}
	kept, err := writeFiles(s.Dir, files)
	if err != nil {
		return err
	}
	for _, k := range kept {
		fmt.Fprintf(stdout, "kept your %s (the scaffold has its own)\n", k)
	}
	if err := tidy(s.Dir, stdout); err != nil {
		fmt.Fprintf(stdout, "warning: couldn't fetch the dependencies yet (%v); run vuka mod tidy when online\n", err)
	}
	return nil
}

// answer fills in the choices: a flag's value, else the question's answer
// (with no prompter, the default).
func (s *scaffold) answer(given map[string]string, p *prompter) error {
	ask := func(key, question string, choices []choice) (string, error) {
		if v := given[key]; v != "" {
			v = synonym(strings.ToLower(v))
			for _, c := range choices {
				if c.key == v {
					return v, nil
				}
			}
			var keys []string
			for _, c := range choices {
				keys = append(keys, c.key)
			}
			return "", fmt.Errorf("--%s=%s: one of %s", key, given[key], strings.Join(keys, ", "))
		}
		if p == nil {
			return choices[0].key, nil
		}
		i, err := p.choose(question, choices, 0)
		if err != nil {
			return "", err
		}
		return choices[i].key, nil
	}
	var err error
	if s.UI, err = ask("ui", "UI", uiChoices); err != nil {
		return err
	}
	if s.DB, err = ask("db", "Database", dbChoices); err != nil {
		return err
	}
	m, err := ask("mail", "Mail", mailChoices)
	if err != nil {
		return err
	}
	s.Mail = m == "yes"
	// Sign-in keeps its users, sessions and tokens in the database: without
	// one, no sign-in, rather than accounts that vanish on every restart.
	if s.DB == "none" {
		switch synonym(strings.ToLower(given["auth"])) {
		case "":
			if p != nil {
				p.note("Authentication", "no: it keeps its users in the database")
			}
		case "no":
		default:
			return errors.New("--auth=yes needs a database: sign-in keeps its users, sessions and tokens there; --db=sqlite needs no setup, or pass --auth=no")
		}
	} else {
		a, err := ask("auth", "Authentication", authChoices)
		if err != nil {
			return err
		}
		s.Auth = a == "yes"
	}
	if s.Frontend, err = ask("frontend", "Frontend app", frontendChoices); err != nil {
		return err
	}
	// The sample Vue island is a client component on a live page, built from
	// its own web/ frontend root; it needs live pages to host it and no Vite
	// frontend to own the root. Offer it only then; refuse --islands=yes
	// otherwise rather than scaffold something that can't build.
	if s.UI == "live" && s.Frontend == "none" {
		i, err := ask("islands", "Vue island sample", islandsChoices)
		if err != nil {
			return err
		}
		s.Islands = i == "yes"
		return nil
	}
	switch synonym(strings.ToLower(given["islands"])) {
	case "", "no":
		if p != nil && given["islands"] == "" {
			p.note("Vue island sample", "no: it needs live pages and no Vite frontend")
		}
	case "yes":
		return errors.New("--islands=yes needs --ui=live and --frontend=none: the sample is a Vue component on a live page, served from its own web/ frontend root")
	default:
		return fmt.Errorf("--islands=%s: one of yes, no", given["islands"])
	}
	return nil
}

// synonym is the choice key a flag's value means.
func synonym(v string) string {
	switch v {
	case "true", "y", "on", "1":
		return "yes"
	case "false", "n", "off", "0":
		return "no"
	case "postgresql", "pg":
		return "postgres"
	case "sqlite3":
		return "sqlite"
	}
	return v
}

// tags are what the path segments' @tags test.
func (s *scaffold) tags() map[string]bool {
	return map[string]bool{
		s.UI: true, "pages": s.UI != "api",
		s.DB: true, "db": s.DB != "none", "nodb": s.DB == "none",
		"auth": s.Auth, "noauth": !s.Auth, "mail": s.Mail,
		s.Frontend: true, "frontend": s.Frontend != "none",
		"islands": s.Islands,
	}
}

// render is the app's files by slash-separated path.
func (s *scaffold) render() (map[string][]byte, error) {
	return renderTree("scaffold/app", s.tags(), s, s.Module)
}

// renderTree renders the files under root of scaffoldFS for tags and
// data; Go and Vuka files of module are formatted, their imports grouped.
func renderTree(root string, tags map[string]bool, data any, module string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := fs.WalkDir(scaffoldFS, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(p, root+"/")
		isTemplate := strings.HasSuffix(rel, ".tmpl")
		out, ok := tagged(strings.TrimSuffix(rel, ".tmpl"), tags)
		if !ok {
			return nil
		}
		b, err := scaffoldFS.ReadFile(p)
		if err != nil {
			return err
		}
		if isTemplate {
			if b, err = execute(rel, b, data); err != nil {
				return err
			}
			if len(bytes.TrimSpace(b)) == 0 {
				return nil
			}
			if b, err = formatSource(out, b, module); err != nil {
				return err
			}
		}
		if _, dup := files[out]; dup {
			return fmt.Errorf("scaffold: two files make %s", out)
		}
		files[out] = b
		return nil
	})
	return files, err
}

// tagged is p without its segments' @tags, and whether every tag holds.
func tagged(p string, tags map[string]bool) (string, bool) {
	segs := strings.Split(p, "/")
	for i, seg := range segs {
		name, want, ok := strings.Cut(seg, "@")
		if !ok {
			continue
		}
		for t := range strings.SplitSeq(want, "+") {
			if !tags[t] {
				return "", false
			}
		}
		segs[i] = name
	}
	return strings.Join(segs, "/"), true
}

var blankRuns = regexp.MustCompile(`\n(?:[ \t]*\n){2,}`)

func execute(name string, src []byte, data any) ([]byte, error) {
	t, err := template.New(name).Delims("{%", "%}").Parse(string(src))
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return nil, err
	}
	// A line holding only an action leaves a blank line: fold the runs.
	return blankRuns.ReplaceAll(b.Bytes(), []byte("\n\n")), nil
}

// formatSource formats a rendered .go or .vuka file as vuka fmt and gofmt
// do, its imports grouped: the standard library's, then other modules',
// then the app's own.
func formatSource(name string, src []byte, module string) ([]byte, error) {
	var formatter func([]byte) ([]byte, error)
	switch path.Ext(name) {
	case ".vuka":
		formatter = format.Source
	case ".go":
		formatter = goformat.Source
	default:
		return src, nil
	}
	out, err := formatter(groupImports(src, module))
	if err != nil {
		return nil, fmt.Errorf("scaffold: %s: %v\n%s", name, err, src)
	}
	return out, nil
}

// groupImports puts a blank line between the groups of the import block:
// the standard library, other modules, module's own packages.
func groupImports(src []byte, module string) []byte {
	start := bytes.Index(src, []byte("\nimport (\n"))
	if start < 0 {
		return src
	}
	start += len("\nimport (\n")
	end := bytes.Index(src[start:], []byte("\n)"))
	if end < 0 {
		return src
	}
	var groups [3][]string
	for line := range strings.SplitSeq(string(src[start:start+end]), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		q := strings.Index(line, `"`)
		if q < 0 {
			return src // something other than imports: leave it
		}
		p, _, _ := strings.Cut(line[q+1:], `"`)
		g := 1
		switch {
		case p == module || strings.HasPrefix(p, module+"/"):
			g = 2
		case !strings.Contains(strings.Split(p, "/")[0], "."):
			g = 0
		}
		groups[g] = append(groups[g], "\t"+line)
	}
	var parts []string
	for _, g := range groups {
		if len(g) > 0 {
			parts = append(parts, strings.Join(g, "\n"))
		}
	}
	return slices.Concat(src[:start], []byte(strings.Join(parts, "\n\n")), src[start+end:])
}

// checkTarget refuses a directory with files in it, unless forced.
func checkTarget(dir string, force bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 || force {
		return nil
	}
	return fmt.Errorf("%s exists and isn't empty: pick another directory, or pass --force to add the app's files beside yours (none of yours is overwritten)", dir)
}

// writeFiles writes files under dir, keeping any file already there with
// other content; it returns the ones kept.
func writeFiles(dir string, files map[string][]byte) (kept []string, err error) {
	var names []string
	for n := range files {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		p := filepath.Join(dir, filepath.FromSlash(n))
		if have, err := os.ReadFile(p); err == nil {
			if !bytes.Equal(have, files[n]) {
				kept = append(kept, n)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return kept, err
		}
		mode := os.FileMode(0o644)
		if path.Base(n) == ".env" {
			mode = 0o600 // it holds secrets
		}
		if err := os.WriteFile(p, files[n], mode); err != nil {
			return kept, err
		}
	}
	return kept, nil
}

// tidy runs vuka mod tidy in dir: go.sum, and the requirements the code
// imports.
func tidy(dir string, w io.Writer) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	c := exec.Command(self, "mod", "tidy")
	c.Dir = dir
	var out bytes.Buffer
	c.Stdout, c.Stderr = &out, &out
	step := task(w, "fetching the app's Go modules", "Go modules ready", "vuka mod tidy failed", "still fetching").Start()
	err = c.Run()
	step.End(err, nil)
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(out.String()))
	}
	return nil
}

// replacesFromEnv is VUKA_NEW_REPLACE, module=dir pairs separated by
// commas: a new app's go.mod replaces those modules with local checkouts,
// for working on vuka new itself.
func replacesFromEnv() map[string]string {
	v := os.Getenv("VUKA_NEW_REPLACE")
	if v == "" {
		return nil
	}
	out := map[string]string{}
	for kv := range strings.SplitSeq(v, ",") {
		if m, d, ok := strings.Cut(kv, "="); ok {
			out[strings.TrimSpace(m)] = strings.TrimSpace(d)
		}
	}
	return out
}

// ReplaceLines are go.mod's replace directives, sorted.
func (s *scaffold) ReplaceLines() []string {
	var out []string
	for m, d := range s.Replace {
		out = append(out, "replace "+m+" => "+d)
	}
	sort.Strings(out)
	return out
}

// Pages reports whether the app serves pages (it isn't an API only).
func (s *scaffold) Pages() bool { return s.UI != "api" }

// Slug is the module's last element as an identifier: the database's
// name in DATABASE_URL, the binary's, the frontend package's.
func (s *scaffold) Slug() string {
	n := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return '_'
	}, path.Base(s.Module))
	if n == "" {
		return "app"
	}
	return n
}

// newPassword is a strong random password: 20 letters and digits.
func newPassword() string {
	const alphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

// displayName is a directory's name as people read it: my-app is My App.
func displayName(dir string) string {
	words := strings.FieldsFunc(dir, func(r rune) bool { return r == '-' || r == '_' || r == ' ' || r == '.' })
	for i, w := range words {
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	if len(words) == 0 {
		return "App"
	}
	return strings.Join(words, " ")
}

func (s *scaffold) summary(w io.Writer) {
	what := map[string]string{"live": "live pages", "templ": "templ views", "api": "a JSON API"}[s.UI]
	if s.DB != "none" {
		what += ", " + map[string]string{"sqlite": "SQLite", "postgres": "PostgreSQL", "mysql": "MySQL"}[s.DB]
	}
	if s.Auth {
		what += ", sign-in"
	}
	if s.Mail {
		what += ", mail"
	}
	if s.Frontend != "none" {
		what += ", a " + displayName(s.Frontend) + " app at /app"
	}
	if s.Islands {
		what += ", a Vue island at /island"
	}
	fmt.Fprintf(w, "\nCreated %s (module %s): %s.\n", s.Dir, s.Module, what)
	if s.Auth {
		fmt.Fprintf(w, "\nThe first admin, created when the app first starts (the password is kept in\n.env, which git ignores):\n\n\temail     %s\n\tpassword  %s\n", s.AdminEmail, s.AdminPassword)
	}
	if s.DB == "postgres" || s.DB == "mysql" {
		fmt.Fprintf(w, "\nCreate the database DATABASE_URL names in .env before the first run.\n")
	}
	fmt.Fprintf(w, "\nNext:\n\n\tcd %s\n\tvuka run -watch .       # then open http://localhost:8080\n\tvuka test ./...\n", s.Dir)
	if s.DB != "none" {
		fmt.Fprintf(w, "\tvuka add module notes   # a model, service, page, migration and test\n")
	}
	if s.Islands {
		fmt.Fprintf(w, "\nThe Vue island at /island is built from web/; install its dependencies\nonce (Node.js 20+), then vuka build bundles it:\n\n\t(cd %s/web && npm install)\n", s.Dir)
	}
}

// interspersed parses args with fs, flags before or after the positional
// arguments, which it returns.
func interspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos, args = append(pos, args[0]), args[1:]
	}
}
