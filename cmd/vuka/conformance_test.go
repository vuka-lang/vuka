package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vuka-lang/ui/live/livetest"
)

// TestConformance runs every app of examples/conformance in each of its
// forms (docs/design/library.md §8) and compares what they answer:
//
//	vuka          the .vuka app, built by vuka build
//	go+gen        the hand-written Go, its speed-ups generated (orm
//	              generate, vuka gen -inplace, vuka build-islands), which
//	              go vet with vukavet passes
//	go            the same without the speed-ups (orm's reflection, Wrap's
//	              reflection, whole-component live diffs)
//	ejected+gen   the .vuka app after vuka eject -rm, built by go build and
//	              passing go vet with vukavet
//	ejected       the same without the speed-ups
//
// An app's script says what to ask: HTTP requests, and live sessions whose
// messages go over the page's socket in both protocols. Each run answers
// with a transcript — statuses, headers, bodies, live replies, the SQL each
// step ran — and every transcript must be the first's; the live render
// trees (protocol 2) are compared among the runs diffing alike.
func TestConformance(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs every example app five ways")
	}
	if bi, ok := debug.ReadBuildInfo(); !ok || !slices.ContainsFunc(bi.Deps, func(m *debug.Module) bool {
		return m.Path == "github.com/vuka-lang/vuka" && m.Replace != nil
	}) {
		needs(t, "needs this checkout's transpiler: go mod edit -replace github.com/vuka-lang/vuka=../..")
	}
	mods := confCheckouts(t)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")
	vet := filepath.Join(t.TempDir(), "vukavet")
	build := exec.Command("go", "build", "-o", vet, ".")
	build.Dir = "../vukavet"
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build vukavet: %v\n%s", err, out)
	}
	scripts, _ := filepath.Glob("../../examples/conformance/*/script")
	if len(scripts) == 0 {
		t.Fatal("no examples/conformance/*/script")
	}
	for _, script := range scripts {
		dir, _ := filepath.Abs(filepath.Dir(script))
		t.Run(filepath.Base(dir), func(t *testing.T) {
			steps := readScript(t, script)
			nm := ""
			if src, _ := os.ReadFile(script); bytes.Contains(src, []byte("\n# needs: node\n")) {
				if _, err := exec.LookPath("node"); err != nil {
					needs(t, "no node")
				}
				if nm = fixtureModules("VUKA_ISLAND_NM", "islands"); nm == "" {
					needs(t, "no island node_modules: npm ci --prefix testdata/node/islands")
				}
			}
			runs := confBuild(t, dir, mods, nm, vet)
			out := os.Getenv("VUKA_CONFORMANCE_OUT")
			var first *confRun
			for _, r := range runs {
				r.transcript, r.trees = r.play(t, steps)
				r.transcript = append(typings(t, r.root), r.transcript...)
				canonSites(r.transcript, r.trees)
				if out != "" {
					writeFile(t, filepath.Join(out, filepath.Base(dir), r.name+".txt"), strings.Join(append(r.transcript, r.trees...), "\n\n"))
				}
				if first == nil {
					first = r
					continue
				}
				if d := firstDiff(first.transcript, r.transcript); d != "" {
					t.Errorf("%s answers differently from %s:\n%s", r.name, first.name, d)
				}
			}
			// The render trees follow the code's shape (a Go if or ui.If, say):
			// they are compared where the code is the same, and among the
			// whole-component runs, which send each component as its HTML.
			for _, pair := range [][2]int{{0, 3}, {2, 4}} {
				o, r := runs[pair[0]], runs[pair[1]]
				if d := firstDiff(o.trees, r.trees); d != "" {
					t.Errorf("%s's live render trees differ from %s's:\n%s", r.name, o.name, d)
				}
			}
		})
	}
}

// confCheckouts are the local checkouts of the vuka modules the apps build
// against: $VUKA_UI, $VUKA_WEB, $VUKA_ORM, else the siblings of this one.
func confCheckouts(t *testing.T) map[string]string {
	repo, _ := filepath.Abs("../..")
	mods := map[string]string{"github.com/vuka-lang/vuka": repo}
	for _, m := range []struct{ name, env string }{{"ui", "VUKA_UI"}, {"web", "VUKA_WEB"}, {"orm", "VUKA_ORM"}} {
		dir := os.Getenv(m.env)
		if dir == "" {
			dir = filepath.Join(filepath.Dir(repo), "vuka-"+m.name)
		}
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
			needs(t, "no checkout of github.com/vuka-lang/%s at %s; set %s", m.name, dir, m.env)
		}
		mods["github.com/vuka-lang/"+m.name] = dir
	}
	return mods
}

// confModule copies an app's form into a module of its own requiring the
// checkouts: each requirement of theirs at the highest version one names,
// so the go command builds it as it is, with nothing to update.
func confModule(t *testing.T, src string, mods map[string]string, extra map[string]string, nodeModules string) string {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if rel == "go.mod" || rel == "go.sum" {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		writeFile(t, filepath.Join(root, rel), string(b))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, s := range extra {
		writeFile(t, filepath.Join(root, name), s)
	}
	req := map[string]string{}
	var sums []byte
	paths := slices.Sorted(func(yield func(string) bool) {
		for p := range mods {
			if !yield(p) {
				return
			}
		}
	})
	for _, p := range paths {
		req[p] = "v0.0.0"
	}
	for _, p := range paths {
		out, err := exec.Command("go", "mod", "edit", "-json", filepath.Join(mods[p], "go.mod")).Output()
		if err != nil {
			t.Fatalf("go mod edit -json %s: %v", mods[p], err)
		}
		var mf struct {
			Require []struct{ Path, Version string }
		}
		if err := json.Unmarshal(out, &mf); err != nil {
			t.Fatal(err)
		}
		for _, r := range mf.Require {
			if v, ok := req[r.Path]; !ok || semverLess(v, r.Version) {
				req[r.Path] = r.Version
			}
		}
		s, _ := os.ReadFile(filepath.Join(mods[p], "go.sum"))
		sums = append(sums, s...)
	}
	var mod strings.Builder
	mod.WriteString("module example.com/app\n\ngo 1.27\n\nrequire (\n")
	for _, p := range slices.Sorted(func(yield func(string) bool) {
		for p := range req {
			if !yield(p) {
				return
			}
		}
	}) {
		fmt.Fprintf(&mod, "\t%s %s\n", p, req[p])
	}
	mod.WriteString(")\n")
	for _, p := range paths {
		fmt.Fprintf(&mod, "\nreplace %s => %s\n", p, mods[p])
	}
	writeFile(t, filepath.Join(root, "go.mod"), mod.String())
	writeFile(t, filepath.Join(root, "go.sum"), string(sums))
	if nodeModules != "" {
		if err := os.Symlink(nodeModules, filepath.Join(root, "node_modules")); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// confRun is one build of an app.
type confRun struct {
	name  string
	bin   string
	root  string
	whole bool // live components diff whole (VUKA_LIVE_WHOLE=1)
	sql   bool // the app has the SQL log route

	transcript, trees []string
}

// confSQL is added to every form of an app using the orm: a route listing
// the statements run since its last call.
const confSQL = `package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/vuka-lang/orm"
	"github.com/vuka-lang/web"
)

var conformanceSQL struct {
	sync.Mutex
	q []string
}

func init() {
	orm.Observe(func(_ context.Context, q orm.QueryInfo) {
		conformanceSQL.Lock()
		defer conformanceSQL.Unlock()
		conformanceSQL.q = append(conformanceSQL.q, fmt.Sprintf("%s %v rows=%d err=%v", q.SQL, q.Args, q.Rows, q.Err))
	})
	web.Routes.Get("/_conformance/sql", ConformanceSQL)
}

// ConformanceSQL is the statements run since the last call.
func ConformanceSQL() []string {
	conformanceSQL.Lock()
	defer conformanceSQL.Unlock()
	q := conformanceSQL.q
	conformanceSQL.q = nil
	return q
}
`

// speedUps are the generated files only making an app faster.
var speedUps = regexp.MustCompile(`^(orm_scanners.*_gen|vuka_calls_gen)\.go$`)

// confBuild builds an app's forms.
func confBuild(t *testing.T, dir string, mods map[string]string, nodeModules, vukavet string) []*confRun {
	t.Helper()
	bin := t.TempDir()
	usesORM := false
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (strings.HasSuffix(p, ".go") || strings.HasSuffix(p, ".vuka")) {
			b, _ := os.ReadFile(p)
			usesORM = usesORM || bytes.Contains(b, []byte(`"github.com/vuka-lang/orm"`))
		}
		return nil
	})
	extra := map[string]string{}
	if usesORM {
		extra["zz_conformance.go"] = confSQL
	}
	goBuild := func(name string) string {
		t.Helper()
		out := filepath.Join(bin, name)
		if b, err := exec.Command("go", "build", "-o", out, ".").CombinedOutput(); err != nil {
			t.Fatalf("%s: go build: %v\n%s", name, err, b)
		}
		if strings.HasSuffix(name, "+gen") {
			if b, err := exec.Command("go", "vet", "-vettool="+vukavet, "./...").CombinedOutput(); err != nil {
				t.Errorf("%s: go vet -vettool=vukavet: %v\n%s", name, err, b)
			}
		}
		return out
	}
	// without builds root again with no speed-ups, removing them; it
	// returns the ones it removed.
	without := func(name, root string) (*confRun, []string) {
		t.Helper()
		var gone []string
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && speedUps.MatchString(d.Name()) {
				rel, _ := filepath.Rel(root, p)
				gone = append(gone, filepath.ToSlash(rel))
				return os.Remove(p)
			}
			return nil
		})
		return &confRun{name: name, bin: goBuild(name), root: root, whole: true, sql: usesORM}, gone
	}
	var runs []*confRun

	root := confModule(t, filepath.Join(dir, "vuka"), mods, extra, nodeModules)
	t.Chdir(root)
	var stderr bytes.Buffer
	if err := runGo("build", []string{"-o", filepath.Join(bin, "vuka"), "."}, io.Discard, &stderr); err != nil {
		t.Fatalf("vuka build: %v\n%s", err, stderr.Bytes())
	}
	runs = append(runs, &confRun{name: "vuka", bin: filepath.Join(bin, "vuka"), root: root, sql: usesORM})

	root = confModule(t, filepath.Join(dir, "go"), mods, extra, nodeModules)
	t.Chdir(root)
	if usesORM {
		if b, err := exec.Command("go", "run", "github.com/vuka-lang/orm/cmd/orm", "generate").CombinedOutput(); err != nil {
			t.Fatalf("orm generate: %v\n%s", err, b)
		}
	}
	if err := gen([]string{"-inplace", "./..."}, io.Discard); err != nil {
		t.Fatalf("vuka gen -inplace: %v", err)
	}
	if hasDir(root, "web/dist") {
		if err := buildIslands(nil, io.Discard, os.Stderr); err != nil {
			t.Fatalf("vuka build-islands: %v", err)
		}
	}
	runs = append(runs, &confRun{name: "go+gen", bin: goBuild("go+gen"), root: root, sql: usesORM})
	run, goSpeedUps := without("go", root)
	runs = append(runs, run)

	root = confModule(t, filepath.Join(dir, "vuka"), mods, extra, nodeModules)
	t.Chdir(root)
	if err := eject([]string{"-rm", "./..."}, io.Discard); err != nil {
		t.Fatalf("vuka eject -rm: %v", err)
	}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".vuka") {
			t.Errorf("vuka eject -rm left %s", p)
		}
		return nil
	})
	var out bytes.Buffer
	if err := gen([]string{"-inplace", "-check", "./..."}, &out); err != nil {
		t.Errorf("vuka gen -inplace -check after vuka eject -rm: %v\n%s", err, out.Bytes())
	}
	if hasDir(root, "web/dist") {
		if err := buildIslands(nil, io.Discard, os.Stderr); err != nil {
			t.Fatalf("vuka build-islands: %v", err)
		}
	}
	runs = append(runs, &confRun{name: "ejected+gen", bin: goBuild("ejected+gen"), root: root, sql: usesORM})
	run, ejectedSpeedUps := without("ejected", root)
	runs = append(runs, run)
	if !slices.Equal(goSpeedUps, ejectedSpeedUps) {
		t.Errorf("the speed-ups generated for the Go app, %v, aren't the ejected app's, %v", goSpeedUps, ejectedSpeedUps)
	}
	return runs
}

// typings are the TypeScript types the vuka command wrote for an app's
// islands and components (vuka build, vuka gen, vuka build-islands), each
// as a transcript entry.
func typings(t *testing.T, root string) []string {
	var out []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() && (d.Name() == "node_modules" || rel == "build" || rel == ".vuka") {
			return filepath.SkipDir
		}
		if !d.IsDir() && typingFile.MatchString(d.Name()) {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, "FILE "+filepath.ToSlash(rel)+"\n"+string(b))
		}
		return nil
	})
	return out
}

var typingFile = regexp.MustCompile(`^(vuka-live|vuka-island|islands)\.d\.ts$`)

func hasDir(root, rel string) bool {
	fi, err := os.Stat(filepath.Join(root, rel))
	return err == nil && fi.IsDir()
}

// A script step: an HTTP request, or a live page with the messages its
// socket sends.
type confStep struct {
	method, path, ctype, body string
	live                      []string // a live page's: JSON to send, or "wait" for a message the server sends unasked
}

// readScript reads an app's script: lines "GET path", "POST path [json|form
// body]" and "LIVE path", the messages a live page sends indented under it
// (JSON; "wait" for a message the server sends of itself). # starts a
// comment.
func readScript(t *testing.T, path string) []confStep {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var steps []confStep
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if s := strings.TrimSpace(line); s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if len(steps) == 0 || steps[len(steps)-1].method != "LIVE" {
				t.Fatalf("%s:%d: a live message outside LIVE", path, n)
			}
			steps[len(steps)-1].live = append(steps[len(steps)-1].live, strings.TrimSpace(line))
			continue
		}
		f := strings.SplitN(line, " ", 4)
		st := confStep{method: f[0]}
		switch {
		case len(f) == 2 && (f[0] == "GET" || f[0] == "POST" || f[0] == "LIVE"):
			st.path = f[1]
		case len(f) == 4 && f[0] == "POST" && (f[2] == "json" || f[2] == "form"):
			st.path, st.ctype, st.body = f[1], map[string]string{"json": "application/json", "form": "application/x-www-form-urlencoded"}[f[2]], f[3]
		default:
			t.Fatalf("%s:%d: %q isn't a step", path, n, line)
		}
		steps = append(steps, st)
	}
	return steps
}

var (
	// assetRef is a /_vuka/ file a page or a script names.
	assetRef = regexp.MustCompile(`(?:src=|href=|")(?:"|)(/_vuka/[^"?\s]+(?:\?v=[^"\s]*)?)"`)
	// manifest is the loader's: the components and islands it imports.
	manifest = regexp.MustCompile(`const info = .*;`)
)

// volatile is what differs between two processes answering alike: a live
// page's join token, a navigation's next one, cookies' values.
var volatile = regexp.MustCompile(`data-vk-(?:token|sig|session)="[^"]*"|,?"token":"[^"]*"`)

// play runs the app and its script: the transcript, and the live render
// trees apart.
func (r *confRun) play(t *testing.T, steps []confStep) (transcript, trees []string) {
	t.Helper()
	env := []string{"VUKA_LIVE_VERIFY=1", "VUKA_ENV=", "VUKA_DEV="}
	if r.whole {
		env = append(env, "VUKA_LIVE_WHOLE=1")
	}
	base, logs := runApp(t, r.bin, r.root, env...)
	defer func() {
		if t.Failed() {
			t.Logf("%s's log:\n%s", r.name, logs)
		}
	}()
	sql := func() string {
		if !r.sql {
			return ""
		}
		res, err := http.Get(base + "/_conformance/sql")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var q []string
		json.NewDecoder(res.Body).Decode(&q)
		if len(q) == 0 {
			return ""
		}
		return "\nsql:\n\t" + strings.Join(q, "\n\t")
	}
	// Each /_vuka/ file a response names (a page's scripts and stylesheets,
	// the islands the loader's manifest names) is fetched once.
	seen := map[string]bool{}
	var assets func(body []byte)
	assets = func(body []byte) {
		t.Helper()
		for _, m := range assetRef.FindAllSubmatch(body, -1) {
			u := string(m[1])
			if seen[u] {
				continue
			}
			seen[u] = true
			res, err := http.Get(base + u)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(res.Body)
			res.Body.Close()
			res.Header.Del("Last-Modified")
			sum := sha256.Sum256(b)
			text := fmt.Sprintf("%d %s\n%d bytes, sha256 %x", res.StatusCode, res.Header.Get("Content-Type"), len(b), sum[:8])
			if m := manifest.Find(b); m != nil {
				text += "\n" + string(m)
			}
			transcript = append(transcript, "ASSET "+u+"\n"+text)
			assets(b)
		}
	}
	do := func(st confStep) []byte {
		t.Helper()
		var req *http.Request
		switch st.method {
		case "POST":
			req, _ = http.NewRequest("POST", base+st.path, strings.NewReader(st.body))
			req.Header.Set("Content-Type", st.ctype)
		default:
			req, _ = http.NewRequest("GET", base+st.path, nil)
		}
		res, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Fatalf("%s %s: %v", st.method, st.path, err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		transcript = append(transcript, st.method+" "+st.path+"\n"+response(res, body)+sql())
		assets(body)
		return body
	}
	sql()
	for _, st := range steps {
		if st.method != "LIVE" {
			do(st)
			continue
		}
		for _, v := range []int{1, 2} {
			body := do(st)
			tok := regexp.MustCompile(`data-vk-token="([^"]*)"`).FindSubmatch(body)
			if tok == nil {
				t.Fatalf("%s: LIVE %s: the page has no live token", r.name, st.path)
			}
			msgs, pages := r.live(t, base, string(tok[1]), v, st.live, sql)
			for i, m := range msgs {
				label := fmt.Sprintf("LIVE %s v%d #%d", st.path, v, i)
				if v == 1 {
					transcript = append(transcript, label+"\n"+m)
					continue
				}
				trees = append(trees, label+"\n"+m)
				transcript = append(transcript, label+": the page\n"+pages[i])
			}
		}
	}
	return transcript, trees
}

// live joins a page's socket with protocol v and sends msgs: each reply,
// and each message the server sends unasked where the script waits for one,
// with the page a client holds once it applied it (livetest's, the
// protocol's reference client).
func (r *confRun) live(t *testing.T, base, token string, v int, msgs []string, sql func() string) (out, pages []string) {
	t.Helper()
	ws, err := dialWS("ws" + strings.TrimPrefix(base, "http") + "/_vuka/live?t=" + token)
	if err != nil {
		t.Fatalf("%s: %v", r.name, err)
	}
	defer ws.Close()
	ref := 0
	client := &livetest.Client{V: v}
	apply := func(m []byte) string {
		t.Helper()
		var fields map[string]json.RawMessage
		json.Unmarshal(m, &fields)
		delete(fields, "js") // the commands it carries are in the transcript; running them needs a session
		b, _ := json.Marshal(fields)
		if err := client.Apply(b); err != nil {
			t.Errorf("%s: live v%d: the client can't apply %s: %v", r.name, v, m, err)
		}
		return volatile.ReplaceAllString(client.HTML(), "")
	}
	read := func(what string, want int) {
		t.Helper()
		for {
			m, err := ws.Read(10 * time.Second)
			if err != nil {
				t.Fatalf("%s: live v%d %s: %v", r.name, v, what, err)
			}
			var head struct{ Ref int }
			json.Unmarshal(m, &head)
			if head.Ref != want {
				if want != 0 && head.Ref == 0 {
					t.Fatalf("%s: live v%d %s: a message the script doesn't wait for: %s", r.name, v, what, m)
				}
				continue
			}
			out = append(out, what+"\n"+volatile.ReplaceAllString(string(m), "")+sql())
			pages = append(pages, apply(m))
			return
		}
	}
	send := func(raw string) {
		t.Helper()
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatalf("live message %s: %v", raw, err)
		}
		ref++
		m["ref"] = ref
		if raw == `{"type":"join"}` && v == 2 {
			m["v"] = 2
		}
		b, _ := json.Marshal(m)
		if err := ws.Send(b); err != nil {
			t.Fatal(err)
		}
		read("→ "+raw, ref)
	}
	send(`{"type":"join"}`)
	for _, m := range msgs {
		if m == "wait" {
			read("(wait)", 0)
			continue
		}
		send(m)
	}
	return out, pages
}

// site is a template hole's place where the page shows it (an island's
// data-vk-s): its source position, which differs from form to form.
var site = regexp.MustCompile(`(data-vk-s=\\?")([^"\\]*:\d+(?:\.\d+)?#\d+)`)

// canonSites names the places a run's pages show by the order they first
// appear in its transcripts, in place: two places are still two, and one
// shown twice is the same.
func canonSites(ts ...[]string) {
	names := map[string]string{}
	for _, t := range ts {
		for i := range t {
			t[i] = site.ReplaceAllStringFunc(t[i], func(m string) string {
				sm := site.FindStringSubmatch(m)
				if names[sm[2]] == "" {
					names[sm[2]] = fmt.Sprintf("site%d", len(names)+1)
				}
				return sm[1] + names[sm[2]]
			})
		}
	}
}

// response is a response as a transcript shows it: its status, headers
// but the date and length, and body, volatile parts left out.
func response(res *http.Response, body []byte) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d\n", res.StatusCode)
	for _, k := range slices.Sorted(func(yield func(string) bool) {
		for k := range res.Header {
			if !yield(k) {
				return
			}
		}
	}) {
		if k == "Date" || k == "Content-Length" { // the body's length: the body is compared
			continue
		}
		for _, v := range res.Header[k] {
			if k == "Set-Cookie" {
				name, rest, _ := strings.Cut(v, "=")
				_, attrs, _ := strings.Cut(rest, ";")
				v = name + "=…;" + attrs
			}
			fmt.Fprintf(&b, "%s: %s\n", k, v)
		}
	}
	b.WriteString("\n")
	b.WriteString(volatile.ReplaceAllString(string(body), ""))
	return b.String()
}

// firstDiff shows where two transcripts part: the first step they answer
// differently, line by line; "" when they agree.
func firstDiff(want, got []string) string {
	for i := range max(len(want), len(got)) {
		var w, g string
		if i < len(want) {
			w = want[i]
		}
		if i < len(got) {
			g = got[i]
		}
		if w != g {
			return lineDiff(w, g)
		}
	}
	return ""
}

// lineDiff is a's and b's lines, those only one has marked - and +.
func lineDiff(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	// lcs[i][j] is the length of the longest common subsequence of al[i:] and bl[j:].
	lcs := make([][]int, len(al)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(bl)+1)
	}
	for i := len(al) - 1; i >= 0; i-- {
		for j := len(bl) - 1; j >= 0; j-- {
			if al[i] == bl[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out strings.Builder
	var gone, added []string
	flush := func() {
		for k := range max(len(gone), len(added)) {
			switch {
			case k >= len(added):
				out.WriteString("- " + gone[k] + "\n")
			case k >= len(gone):
				out.WriteString("+ " + added[k] + "\n")
			default:
				g, a := around(gone[k], added[k])
				out.WriteString("- " + g + "\n+ " + a + "\n")
			}
		}
		gone, added = nil, nil
	}
	i, j := 0, 0
	for i < len(al) || j < len(bl) {
		switch {
		case i < len(al) && j < len(bl) && al[i] == bl[j]:
			flush()
			out.WriteString("  " + short(al[i]) + "\n")
			i++
			j++
		case j < len(bl) && (i == len(al) || lcs[i][j+1] >= lcs[i+1][j]):
			added = append(added, bl[j])
			j++
		default:
			gone = append(gone, al[i])
			i++
		}
	}
	flush()
	return out.String()
}

// short is a line cut to a width a diff can show.
func short(s string) string {
	if len(s) > 160 {
		return s[:150] + "…"
	}
	return s
}

// around is two lines differing, each cut to where they part.
func around(a, b string) (string, string) {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	cut := func(s string) string {
		from, to := max(0, i-60), min(len(s), i+100)
		out := s[from:to]
		if from > 0 {
			out = "…" + out
		}
		if to < len(s) {
			out += "…"
		}
		return out
	}
	return cut(a), cut(b)
}
