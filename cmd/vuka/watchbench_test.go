package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestWatchBench measures what a rebuild under vuka run -watch costs, by
// the steps VUKA_WATCH_TRACE reports, for the saves of #310: text in one
// page, text in a shared component, a new unused function in a widely
// imported package, and a change to that package's exported API. It runs
// only with VUKA_WATCH_BENCH set (it takes minutes):
//
//	VUKA_WATCH_BENCH=1 go test -run TestWatchBench -v -timeout 60m
//
// It generates an app of about the size of a real one (VUKA_WATCH_BENCH_SCALE
// sections of pages, 36 by default: about 440 vuka files) against the ui and
// web checkouts the other end-to-end tests use. VUKA_WATCH_BENCH_APP names an
// existing app to measure instead, with VUKA_WATCH_BENCH_EDITS its saves:
// "name|kind|file|anchor" separated by ";", kind one of text (anchor gets
// a marker appended), func (an unexported function is added), export (an
// exported one), const (an exported constant) and replace (anchor is
// old=>new, {id} in new making each run's different). The app is edited and
// put back. VUKA_WATCH_BENCH_RUNS is how many times each save runs (2), and
// VUKA_WATCH_BENCH_FLAGS the flags vuka run gets ("-p=2") and
// VUKA_WATCH_BENCH_ENV more of its environment; the build runs with
// GOMAXPROCS=4 unless it is set. VUKA_WATCH_BENCH_OUT=dir only writes
// the generated app there.
func TestWatchBench(t *testing.T) {
	t.Parallel()
	if testing.Short() || os.Getenv("VUKA_WATCH_BENCH") == "" {
		t.Skip("set VUKA_WATCH_BENCH to measure rebuilds")
	}
	vuka := os.Getenv("VUKA_WATCH_BENCH_VUKA") // a vuka built already, as from another checkout
	if vuka == "" {
		vuka = buildVuka(t)
	}
	root := os.Getenv("VUKA_WATCH_BENCH_APP")
	var edits []benchEdit
	if root != "" {
		var err error
		if edits, err = parseBenchEdits(os.Getenv("VUKA_WATCH_BENCH_EDITS")); err != nil {
			t.Fatal(err)
		}
	} else {
		scale := 36
		if s := os.Getenv("VUKA_WATCH_BENCH_SCALE"); s != "" {
			scale, _ = strconv.Atoi(s)
		}
		files := benchApp(scale)
		root = localModule(t, webCheckout(t), files)
		if out := os.Getenv("VUKA_WATCH_BENCH_OUT"); out != "" {
			// Only the app, written to look at or to run by hand.
			for name, src := range files {
				writeFile(t, filepath.Join(out, name), src)
			}
			return
		}
		edits = benchEdits
	}
	runs := 2
	if s := os.Getenv("VUKA_WATCH_BENCH_RUNS"); s != "" {
		runs, _ = strconv.Atoi(s)
	}
	flags := strings.Fields(os.Getenv("VUKA_WATCH_BENCH_FLAGS"))
	if os.Getenv("VUKA_WATCH_BENCH_FLAGS") == "" {
		flags = []string{"-p=2"}
	}
	env := append([]string{"VUKA_WATCH_TRACE=1"}, strings.Fields(os.Getenv("VUKA_WATCH_BENCH_ENV"))...)
	if os.Getenv("VUKA_WATCH_BENCH_APP") == "" {
		env = append(env, "GOFLAGS=-mod=mod") // the generated app's go.sum is the checkouts'
	}
	if os.Getenv("GOMAXPROCS") == "" {
		env = append(env, "GOMAXPROCS=4")
	}
	b := startBench(t, vuka, root, env, flags)
	first := b.next(t)
	t.Logf("first build: %s", first)

	// Each save is new to the build cache and to vuka's: no run of the
	// benchmark finds another's.
	nonce := strconv.FormatInt(time.Now().UnixNano()%1e9, 36)
	var rows []string
	for _, e := range edits {
		path := filepath.Join(root, filepath.FromSlash(e.file))
		orig, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.WriteFile(path, orig, 0o644) }) // put back however the run ends
		for i := 1; i <= runs; i++ {
			src, err := e.apply(string(orig), nonce, i)
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, path, src)
			saved := time.Now()
			st := b.next(t)
			if st.total == 0 {
				st.total = time.Since(saved).Seconds() // unchanged: no message says how long it took
			}
			rows = append(rows, fmt.Sprintf("| %s #%d | %s |", e.name, i, st.row()))
		}
		writeFile(t, path, string(orig))
		b.next(t) // back as it was; not counted
	}
	b.stop()
	t.Logf("\n| Save | Transpiled | Transpile | Go compile + link | Prewarm | Total |\n|---|---|---|---|---|---|\n%s", strings.Join(rows, "\n"))
}

// benchEdit is a save TestWatchBench measures.
type benchEdit struct {
	name, kind, file, anchor string
}

// benchEdits are the saves on the generated app.
var benchEdits = []benchEdit{
	{"text in one page", "text", "pages/s7/detail.vuka.html", "Detail of section 7"},
	{"Go in one page", "text", "pages/s7/detail.vuka.html", `return "Detail 7`},
	{"text in a shared component", "text", "components/c3.vuka.html", "Component 3 says"},
	{"new unused func, widely imported package", "func", "models/model3.vuka", ""},
	{"new exported func, widely imported package", "export", "models/model3.vuka", ""},
	{"new exported const, widely imported package", "const", "models/model3.vuka", ""},
	{"exported API change, widely imported package", "replace", "models/model3.vuka", "func Total3(items []Item3) int {=>func Total3(items []Item3, bench_{id} ...int) int {"},
}

func parseBenchEdits(spec string) ([]benchEdit, error) {
	var out []benchEdit
	for _, s := range strings.Split(spec, ";") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		f := strings.SplitN(s, "|", 4)
		if len(f) < 3 {
			return nil, fmt.Errorf("VUKA_WATCH_BENCH_EDITS: %q isn't name|kind|file|anchor", s)
		}
		e := benchEdit{name: f[0], kind: f[1], file: f[2]}
		if len(f) == 4 {
			e.anchor = f[3]
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("VUKA_WATCH_BENCH_EDITS names no saves")
	}
	return out, nil
}

// apply is src with the edit's i-th change: each run changes the source
// again.
func (e benchEdit) apply(src, nonce string, i int) (string, error) {
	var decl string
	id := fmt.Sprintf("%s_%d", nonce, i)
	switch e.kind {
	case "text":
		if !strings.Contains(src, e.anchor) {
			return "", fmt.Errorf("%s has no %q", e.file, e.anchor)
		}
		return strings.Replace(src, e.anchor, fmt.Sprintf("%s (edit %s)", e.anchor, id), 1), nil
	case "func":
		decl = fmt.Sprintf("\nfunc benchUnused%s() int { return %d }\n", id, i)
	case "export":
		decl = fmt.Sprintf("\nfunc BenchUnused%s() int { return %d }\n", id, i)
	case "const":
		decl = fmt.Sprintf("\nconst BenchAPI%s = %d\n", id, i)
	case "replace":
		old, new, ok := strings.Cut(e.anchor, "=>")
		if !ok || !strings.Contains(src, old) {
			return "", fmt.Errorf("%s has no %q (replace's anchor is old=>new)", e.file, old)
		}
		return strings.Replace(src, old, strings.ReplaceAll(new, "{id}", id), 1), nil
	default:
		return "", fmt.Errorf("unknown kind of save %q", e.kind)
	}
	if strings.HasSuffix(e.file, ".vuka.html") {
		at := strings.Index(src, "</script>")
		if at < 0 {
			return "", fmt.Errorf("%s has no script", e.file)
		}
		return src[:at] + decl + src[at:], nil
	}
	return src + decl, nil
}

// benchRun is vuka run -watch with VUKA_WATCH_TRACE set, its output read
// build by build.
type benchRun struct {
	cmd   *exec.Cmd
	lines chan string
}

func startBench(t *testing.T, vuka, root string, env, flags []string) *benchRun {
	t.Helper()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cmd := exec.Command(vuka, append(append([]string{"run", "--watch"}, flags...), ".")...)
	cmd.Dir = root
	cmd.Env = append(append(os.Environ(), fmt.Sprint("PORT=", port), twEnv+"=off"), env...)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	appGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	b := &benchRun{cmd: cmd, lines: make(chan string, 1024)}
	t.Cleanup(func() {
		interrupt(cmd, syscall.SIGTERM)
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			cmd.Process.Kill()
		}
	})
	go func() {
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			if os.Getenv("VUKA_WATCH_BENCH_ECHO") != "" {
				fmt.Fprintln(os.Stderr, sc.Text())
			}
			b.lines <- sc.Text()
		}
		close(b.lines)
	}()
	return b
}

func (b *benchRun) stop() { interrupt(b.cmd, syscall.SIGTERM) }

// benchSteps is what one build's trace says.
type benchSteps struct {
	transpiled         string
	transpile, build   time.Duration
	prewarm            time.Duration
	total              float64 // seconds, from the save to the new version answering
	unchanged, failed  bool
	exited, hasPrewarm bool
}

var (
	traceTranspiled = regexp.MustCompile(`^trace \S+ transpiled (\d+) of (\d+) packages`)
	traceBuild      = regexp.MustCompile(`^trace \S+ transpile (\S+) build (\S+)`)
	tracePrewarm    = regexp.MustCompile(`^trace \S+ prewarm (\S+)`)
	traceRebuilt    = regexp.MustCompile(`^● rebuilt in ([\d.]+)s`)
)

// next reads the output to the end of the next build.
func (b *benchRun) next(t *testing.T) benchSteps {
	t.Helper()
	var st benchSteps
	timeout := time.After(15 * time.Minute)
	var tail []string
	for {
		var line string
		var ok bool
		select {
		case line, ok = <-b.lines:
		case <-timeout:
			t.Fatalf("no build in 15 minutes; output:\n%s", strings.Join(tail, "\n"))
		}
		if !ok {
			t.Fatalf("vuka run -watch exited; output:\n%s", strings.Join(tail, "\n"))
		}
		if tail = append(tail, line); len(tail) > 60 {
			tail = tail[1:]
		}
		if m := traceTranspiled.FindStringSubmatch(line); m != nil {
			st.transpiled = m[1] + "/" + m[2]
		}
		if m := traceBuild.FindStringSubmatch(line); m != nil {
			st.transpile, _ = time.ParseDuration(m[1])
			st.build, _ = time.ParseDuration(m[2])
		}
		if m := tracePrewarm.FindStringSubmatch(line); m != nil {
			st.prewarm, _ = time.ParseDuration(m[1])
			st.hasPrewarm = true
		}
		switch m := traceRebuilt.FindStringSubmatch(line); {
		case m != nil:
			st.total, _ = strconv.ParseFloat(m[1], 64)
			return st
		case strings.HasPrefix(line, "● unchanged"):
			st.unchanged = true
			return st
		case strings.HasPrefix(line, "● build failed"):
			t.Fatalf("a build failed; output:\n%s", strings.Join(tail, "\n"))
		case strings.HasPrefix(line, "● the app exited"):
			// An app that can't start here (no database) is measured all
			// the same: its build is what counts.
			if st.hasPrewarm {
				return st
			}
		case strings.HasPrefix(line, "listening") || strings.Contains(line, "web: listening"):
			if st.hasPrewarm && st.total == 0 {
				return st // the first build: no change to count from
			}
		}
	}
}

func (st benchSteps) String() string { return st.row() }

func (st benchSteps) row() string {
	s := func(d time.Duration) string { return fmt.Sprintf("%.2f s", d.Seconds()) }
	pre := "none"
	if st.hasPrewarm {
		pre = s(st.prewarm)
	}
	total := fmt.Sprintf("%.1f s", st.total)
	if st.total == 0 {
		total = "-"
	} else if st.unchanged {
		total += " (binary unchanged)"
	}
	return fmt.Sprintf("%s | %s | %s | %s | %s", st.transpiled, s(st.transpile), s(st.build), pre, total)
}

// benchApp is an app of sections×8 pages, 40 shared components, 10 module
// packages of 10 files each and a models package every other package
// imports.
func benchApp(sections int) map[string]string {
	const nModels, nComponents, nModules = 12, 40, 10
	files := map[string]string{}
	var b strings.Builder

	// models: what every other package imports.
	files["models/store.vuka"] = `// Package models is the app's data, which every page imports.
package models

import "sync"

// Store holds every model, in memory.
type Store struct {
	mu   sync.Mutex
	seed int
}

func NewStore() *Store { return &Store{seed: 7} }
`
	for k := range nModels {
		files[fmt.Sprintf("models/model%d.vuka", k)] = fmt.Sprintf(`package models

import (
	"fmt"
	"strings"
)

// Item%[1]d is a record of kind %[1]d.
type Item%[1]d struct {
	ID    int
	Name  string
	Price int
	Tags  []string
	Done  bool
}

// Label is how pages show the item.
func (i Item%[1]d) Label() string { return fmt.Sprintf("%%s #%%d", i.Name, i.ID) }

// Cost is its price, formatted.
func (i Item%[1]d) Cost() string { return fmt.Sprintf("%%d.%%02d", i.Price/100, i.Price%%100) }

// Items%[1]d lists the records of kind %[1]d.
func (s *Store) Items%[1]d() []Item%[1]d {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Item%[1]d, 0, 8)
	for n := range 8 {
		out = append(out, Item%[1]d{ID: n + s.seed, Name: strings.Repeat("x", n%%3+1), Price: n * 250, Tags: []string{"a", "b"}, Done: n%%2 == 0})
	}
	return out
}

// Find%[1]d is one record of kind %[1]d.
func (s *Store) Find%[1]d(id int) (Item%[1]d, bool) {
	for _, it := range s.Items%[1]d() {
		if it.ID == id {
			return it, true
		}
	}
	return Item%[1]d{}, false
}

// Total%[1]d adds up the prices.
func Total%[1]d(items []Item%[1]d) int {
	t := 0
	for _, it := range items {
		t += it.Price
	}
	return t
}
`, k)
	}

	// components: shared by every page and module.
	files["components/class.vuka"] = `package components

import "strings"

// cx joins class names, skipping empty ones.
func cx(names ...string) string {
	var out []string
	for _, n := range names {
		if n != "" {
			out = append(out, n)
		}
	}
	return strings.Join(out, " ")
}
`
	for c := range nComponents {
		k := c % nModels
		files[fmt.Sprintf("components/c%d.vuka.html", c)] = fmt.Sprintf(`<script lang="go">
package components

import (
	"example.com/app/models"
	"github.com/vuka-lang/ui/v2"
)

// C%[1]d is shared component %[1]d.
type C%[1]d struct {
	Title    string
	Items    []models.Item%[2]d
	Wide     bool
	Children ui.Node
}

func (c C%[1]d) count() int { return len(c.Items) }
</script>

<section class={cx("card", map[bool]string{true: "wide"}[Wide])}>
  <h2 class="title">{Title}</h2>
  <p class="lead">Component %[1]d says hello, with {count()} items.</p>
  if count() == 0 {
    <p class="empty">Nothing here yet.</p>
  } else {
    <ul>
      for _, it := range Items {
        <li class={cx("row", map[bool]string{true: "done"}[it.Done])}>
          <span class="name">{it.Label()}</span>
          <span class="cost">{it.Cost()}</span>
          for _, tag := range it.Tags {
            <em class="tag">{tag}</em>
          }
        </li>
      }
    </ul>
  }
  <footer>{Children}</footer>
</section>
`, c, k)
	}

	// modules: services and views over the models, as feature packages.
	for m := range nModules {
		k := m % nModels
		files[fmt.Sprintf("modules/m%d/service.vuka", m)] = fmt.Sprintf(`// Package m%[1]d is feature %[1]d.
package m%[1]d

import "example.com/app/models"

// Service is feature %[1]d's logic.
type Service struct {
	store *models.Store
}

func NewService(s *models.Store) *Service { return &Service{store: s} }

// List is what the feature shows.
func (s *Service) List() []models.Item%[2]d { return s.store.Items%[2]d() }

// Find is one of them.
func (s *Service) Find(id int) (models.Item%[2]d, bool) { return s.store.Find%[2]d(id) }

// Total is their total price.
func (s *Service) Total() int { return models.Total%[2]d(s.List()) }
`, m, k)
		for v := range 9 {
			c := (m*9 + v) % nComponents
			ck := c % nModels
			files[fmt.Sprintf("modules/m%d/view%d.vuka.html", m, v)] = fmt.Sprintf(`<script lang="go">
package m%[1]d

import (
	"example.com/app/components"
	"example.com/app/models"
)

// View%[2]d is a view of feature %[1]d.
type View%[2]d struct {
	Svc   *Service
	Extra []models.Item%[4]d
}
</script>

<div class="view">
  <h3>Feature %[1]d, view %[2]d</h3>
  <components.C%[3]d Title="Listed" Items={Extra}>
    <p>Total {Svc.Total()}</p>
    for _, it := range Svc.List() {
      <a href={"/items/" + it.Name}>{it.Label()}</a>
    }
  </components.C%[3]d>
</div>
`, m, v, c, ck)
		}
	}

	// layouts and pages
	files["pages/_layout.vuka.html"] = `<script lang="go">
package pages

import (
	"context"

	"github.com/vuka-lang/ui/v2"
)

// Layout is every page's document.
type Layout struct {
	Children ui.Node
}

func (l Layout) Title(ctx context.Context) string {
	if t := ui.PageTitle(ctx); t != "" {
		return t + " - Bench"
	}
	return "Bench"
}
</script>

<html lang="en">
<head><meta charset="utf-8"/><title>{Title(ctx)}</title></head>
<body><main>{Children}</main></body>
</html>
`
	files["pages/index.vuka.html"] = `<script lang="go">
package pages

// Index is the home page.
type Index struct{}

func (p *Index) Title() string { return "Home" }
</script>

<h1>Home</h1>
`
	names := []string{"index", "list", "detail", "edit", "about", "stats", "help", "more"}
	for sct := range sections {
		m := sct % nModules
		k := m % nModels
		for pi, name := range names {
			typ := strings.ToUpper(name[:1]) + name[1:]
			c1, c2 := (sct+pi)%nComponents, (sct*3+pi)%nComponents
			live := ""
			if pi%2 == 1 {
				live = "\n\tui.Live\n\topen bool"
			}
			toggle := ""
			if pi%2 == 1 {
				toggle = "\n// Toggle opens and closes the details.\nfunc (p *" + typ + ") Toggle() { p.open = !p.open }\n"
			}
			button := ""
			if pi%2 == 1 {
				button = "\n  <button onClick={Toggle}>Toggle</button>\n  if open {\n    <p>Open details of section " + strconv.Itoa(sct) + ".</p>\n  }"
			}
			b.Reset()
			fmt.Fprintf(&b, `<script lang="go">
package s%[1]d

import (
	"example.com/app/components"
	"example.com/app/models"
	"example.com/app/modules/m%[2]d"
	"github.com/vuka-lang/ui/v2"
)

// %[3]s is page %[4]s of section %[1]d.
type %[3]s struct {%[8]s
	svc   *m%[2]d.Service
	store *models.Store
}

func (p *%[3]s) Title() string { return "%[3]s %[1]d" }

func (p *%[3]s) items() []models.Item%[5]d { return p.svc.List() }
%[9]s
var _ ui.Node
</script>

<article>
  <h1>%[3]s of section %[1]d</h1>
  <p>Total: {svc.Total()}</p>%[10]s
  <components.C%[6]d Title="First" Items={nil}>
    <m%[2]d.View%[11]d Svc={svc} />
  </components.C%[6]d>
  <components.C%[7]d Title="Second" Wide={true}>
    for _, it := range items() {
      if it.Done {
        <p class="done">{it.Label()} is done</p>
      } else {
        <p>{it.Label()} costs {it.Cost()}</p>
      }
    }
  </components.C%[7]d>
</article>
`, sct, m, typ, name, k, c1, c2, live, toggle, button, pi)
			files[fmt.Sprintf("pages/s%d/%s.vuka.html", sct, name)] = b.String()
		}
	}

	b.Reset()
	b.WriteString("package main\n\nimport (\n\t\"example.com/app/models\"\n")
	for m := range nModules {
		fmt.Fprintf(&b, "\t\"example.com/app/modules/m%d\"\n", m)
	}
	b.WriteString("\t\"example.com/app/pages\"\n\t\"github.com/vuka-lang/web/v2\"\n)\n\nfunc main() {\n\tweb.New(pages.Routes(), web.Provide(models.NewStore)")
	for m := range nModules {
		fmt.Fprintf(&b, ", web.Provide(m%d.NewService)", m)
	}
	b.WriteString(").Run(\"\")\n}\n")
	files["main.go"] = b.String()
	return files
}
