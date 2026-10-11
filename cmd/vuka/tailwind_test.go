package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vuka-lang/vuka/transpile"
)

func tempModule(t *testing.T, files map[string]string) string {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	for name, src := range files {
		writeFile(t, filepath.Join(root, name), src)
	}
	return root
}

const webMod = "module example.com/app\n\ngo 1.25.0\n\nrequire github.com/vuka-lang/web v0.5.0\n"

func TestTailwindFor(t *testing.T) {
	ui := "package main\n\nimport \"github.com/vuka-lang/ui\"\n\nvar _ ui.Node\n"
	sheet := "@import \"tailwindcss\";\n@theme { --color-brand: #123456; }\n"
	for _, c := range []struct {
		name  string
		files map[string]string
		env   string
		want  string // "-": no Tailwind; "": the built-in stylesheet; else the project's
	}{
		{"no web", map[string]string{"go.mod": "module x\n", "main.vuka": ui}, "", "-"},
		{"no views", map[string]string{"go.mod": webMod, "main.go": "package main\n\nfunc main() {}\n"}, "", "-"},
		{"jsx", map[string]string{"go.mod": webMod, "main.vuka": ui}, "", ""},
		{"templ", map[string]string{"go.mod": webMod, "views/page.templ": "package views\n"}, "", ""},
		{"html", map[string]string{"go.mod": webMod, "views/page.html": "<p></p>"}, "", ""},
		{"off", map[string]string{"go.mod": webMod, "main.vuka": ui}, "off", "-"},
		{"NoTailwind", map[string]string{"go.mod": webMod, "main.vuka": ui + "\nvar o = web.NoTailwind()\n"}, "", "-"},
		{"NoTailwind in a test", map[string]string{"go.mod": webMod, "main.vuka": ui, "main_test.go": "var o = web.NoTailwind()"}, "", ""},
		{"app.css", map[string]string{"go.mod": webMod, "app.css": sheet}, "", "app.css"},
		{"styles/app.css", map[string]string{"go.mod": webMod, "styles/app.css": sheet, "main.vuka": ui}, "", "styles/app.css"},
		{"plain app.css", map[string]string{"go.mod": webMod, "app.css": "p { color: red }", "main.vuka": ui}, "", ""},
		{"named", map[string]string{"go.mod": webMod, "app.css": sheet, "site/main.css": sheet,
			"main.vuka": ui + "\nvar o = web.Tailwind(\"site/main.css\")\n"}, "", "site/main.css"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(twEnv, c.env)
			root := tempModule(t, c.files)
			tw, err := tailwindFor(root)
			switch {
			case err != nil:
				t.Fatal(err)
			case tw == nil && c.want != "-", tw != nil && c.want == "-":
				t.Fatalf("tailwindFor: %v, want %q", tw, c.want)
			case tw != nil && c.want == "" && tw.sheet != "", tw != nil && c.want != "" && tw.sheet != filepath.Join(root, c.want):
				t.Errorf("sheet %q, want %q", tw.sheet, c.want)
			}
		})
	}
	root := tempModule(t, map[string]string{"go.mod": webMod, "main.vuka": ui + "\nvar o = web.Tailwind(\"gone.css\")\n"})
	if _, err := tailwindFor(root); err == nil || !strings.Contains(err.Error(), `web.Tailwind("gone.css")`) {
		t.Errorf("a missing named stylesheet: %v", err)
	}
}

func TestTailwindEntry(t *testing.T) {
	root := tempModule(t, map[string]string{"go.mod": webMod, "app.css": `@import "tailwindcss";`,
		"lib/a/button.vuka": "package a", "lib/b/b.go": "package b", "lib/c/card.templ": "package c", "lib/d/d_test.vuka": "package d"})
	lib := func(d string) string { return filepath.Join(root, "lib", d) }
	deps := componentDirs([]string{lib("a"), lib("b"), lib("c"), lib("d"), lib("a")})
	if want := []string{lib("a"), lib("c")}; fmt.Sprint(deps) != fmt.Sprint(want) {
		t.Errorf("componentDirs: %v, want %v", deps, want)
	}
	for _, sheet := range []string{filepath.Join(root, "app.css"), ""} {
		entry, err := (&tailwind{root: root, sheet: sheet}).writeEntry(deps, nil)
		if err != nil {
			t.Fatal(err)
		}
		imp := `@import "tailwindcss";`
		if sheet != "" {
			imp = fmt.Sprintf("@import %q;", sheet)
		}
		got, _ := os.ReadFile(entry)
		for _, want := range []string{imp, fmt.Sprintf("@source %q;", root+"/**/*.{vuka,templ,html,go}"),
			fmt.Sprintf("@source %q;", lib("a")+"/*.{vuka,templ,go}"), fmt.Sprintf("@source %q;", lib("c")+"/*.{vuka,templ,go}")} {
			if !strings.Contains(string(got), want+"\n") {
				t.Errorf("the entry lacks %s:\n%s", want, got)
			}
		}
	}
	if ig, _ := os.ReadFile(filepath.Join(root, ".vuka", ".gitignore")); string(ig) != "*\n" {
		t.Errorf(".vuka/.gitignore: %q", ig)
	}
}

func TestTailwindBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := tempModule(t, map[string]string{"go.mod": webMod})
	if _, err := tailwindBinary(root); err == nil || err.Error() != twInstall {
		t.Errorf("no Tailwind: %v", err)
	}
	for _, dir := range []string{"frontend", "."} {
		bin := filepath.Join(root, dir, "node_modules", ".bin", "tailwindcss")
		writeFile(t, bin, "#!/bin/sh\n")
		if got, err := tailwindBinary(root); err != nil || got != bin {
			t.Errorf("%s: %q %v, want %s", dir, got, err, bin)
		}
	}
}

// TestDevTailwind runs a stand-in tailwindcss that records its arguments and
// whether it saw its stdin close, as the real one stops when it does.
func TestDevTailwind(t *testing.T) {
	bin, goDir := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(bin, "tailwindcss"), strings.ReplaceAll(`#!/bin/sh
echo "$@" > BIN/args
out=""; while [ $# -gt 0 ]; do [ "$1" = -o ] && out="$2"; shift; done
echo '.bg-red-500{}' > "$out"
echo "≈ tailwindcss v4"
echo "Done in 5ms" >&2
echo "a warning" >&2
/bin/cat > /dev/null
echo stopped > BIN/stopped
`, "BIN", bin))
	os.Chmod(filepath.Join(bin, "tailwindcss"), 0o755)
	goBin, _ := exec.LookPath("go")
	os.Symlink(goBin, filepath.Join(goDir, "go"))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+goDir)
	root := tempModule(t, map[string]string{"go.mod": webMod, "app.css": `@import "tailwindcss";`})
	var log syncBuffer
	css, stop := devTailwind(root, "", &log, nil)
	if want := filepath.Join(root, ".vuka", "tailwind", "app.css"); css != want {
		t.Fatalf("stylesheet %s, want %s", css, want)
	}
	waitFor(t, func() bool { b, _ := os.ReadFile(css); return string(b) == ".bg-red-500{}\n" })
	stop()
	stop()
	if b, _ := os.ReadFile(filepath.Join(bin, "stopped")); string(b) != "stopped\n" {
		t.Errorf("tailwindcss didn't see its stdin close: %q", b)
	}
	args, _ := os.ReadFile(filepath.Join(bin, "args"))
	if want := "-i " + filepath.Join(root, ".vuka", "tailwind", "input.css") + " -o " + css + " --watch\n"; string(args) != want {
		t.Errorf("args %q, want %q", args, want)
	}
	if out := log.String(); !strings.Contains(out, "tailwind: a warning\n") || strings.Contains(out, "Done in") || strings.Contains(out, "≈") {
		t.Errorf("output:\n%s", out)
	}

	t.Setenv("PATH", goDir)
	log.Reset()
	if css, stop := devTailwind(root, "", &log, nil); css != "" || !strings.Contains(log.String(), twInstall) {
		t.Errorf("without Tailwind: %q, %q", css, log.String())
	} else {
		stop()
	}
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuffer) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }
func (s *syncBuffer) Reset()         { s.mu.Lock(); defer s.mu.Unlock(); s.b.Reset() }

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); !ok(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal("timed out")
		}
	}
}

// webModule writes a module requiring the local web ($VUKA_WEB, else the
// sibling checkout ../vuka-web) and vuka (this repository), skipping the test
// when that web doesn't serve Tailwind CSS yet.
func webModule(t *testing.T, files map[string]string) string {
	t.Helper()
	repo, _ := filepath.Abs("../..")
	web := os.Getenv("VUKA_WEB")
	if web == "" {
		web = filepath.Join(filepath.Dir(repo), "vuka-web")
	}
	if !hasRegisterCSS(web) {
		t.Skipf("github.com/vuka-lang/web at %s has no RegisterCSS; set VUKA_WEB", web)
	}
	webSum, _ := os.ReadFile(filepath.Join(web, "go.sum"))
	ownSum, _ := os.ReadFile("go.sum")
	files["go.mod"] = "module example.com/app\n\ngo 1.25.0\n\nrequire (\n\tgithub.com/vuka-lang/web v0.0.0\n\tgithub.com/vuka-lang/vuka " +
		transpile.RuntimeVersion + "\n)\n\nreplace github.com/vuka-lang/web => " + web + "\n\nreplace github.com/vuka-lang/vuka => " + repo + "\n"
	files["go.sum"] = string(webSum) + string(ownSum)
	return tempModule(t, files)
}

const twPage = `package main

import (
	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

@web.Get("/")
func Home() ui.Node {
	return <html><head><title>t</title></head><body>
		<p ref="note" class="bg-emerald-950 text-brand">hi</p>
		<button onClick={ui.JS.AddClass(ui.Ref("note"), "opacity-35")}>go</button>
	</body></html>
}

func main() { web.New().Run("") }
`

// TestBuildTailwind: vuka build embeds the stylesheet, with the classes of a
// .vuka file's JSX and of a JS command and the project's theme, in a binary
// that links it from its pages; the source tree gets no generated file.
func TestBuildTailwind(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	if _, err := exec.LookPath("tailwindcss"); err != nil {
		t.Skip("no tailwindcss on PATH")
	}
	root := webModule(t, map[string]string{"main.vuka": twPage,
		"styles/app.css": "@import \"tailwindcss\";\n@theme { --color-brand: #123456; }\n"})
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv(twEnv, "")
	t.Chdir(root)
	app := filepath.Join(t.TempDir(), "app")
	var stderr bytes.Buffer
	if err := runGo("build", []string{"-o", app, "."}, io.Discard, &stderr); err != nil {
		t.Fatalf("vuka build: %v\n%s", err, stderr.Bytes())
	}
	if _, err := os.Stat(filepath.Join(root, twGoName)); err == nil {
		t.Errorf("vuka build wrote %s into the source tree", twGoName)
	}
	if gen, err := os.ReadFile(filepath.Join(root, "build", twGoName)); err != nil || !bytes.Contains(gen, []byte("RegisterCSS")) {
		t.Errorf("build/%s: %v", twGoName, err)
	}

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cmd := exec.Command(app)
	cmd.Env = append(os.Environ(), fmt.Sprint("PORT=", port), twCSSEnv+"=")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	get := func(path string) (string, http.Header) {
		var res *http.Response
		waitFor(t, func() bool {
			var err error
			res, err = http.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
			return err == nil
		})
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return string(b), res.Header
	}
	page, _ := get("/")
	i := strings.Index(page, `<link rel="stylesheet" href="/_vuka/app.css?v=`)
	if i < 0 || i > strings.Index(page, "</head>") {
		t.Fatalf("the page doesn't link the stylesheet in its head: %s", page)
	}
	href := page[i+len(`<link rel="stylesheet" href="`):]
	href = href[:strings.IndexByte(href, '"')]
	css, hdr := get(href)
	for _, want := range []string{".bg-emerald-950", ".opacity-35", ".text-brand", "#123456"} {
		if !strings.Contains(css, want) {
			t.Errorf("the stylesheet lacks %s", want)
		}
	}
	if !strings.Contains(hdr.Get("Cache-Control"), "immutable") || strings.Contains(css, "\n  ") {
		t.Errorf("not immutable or not minified: %v", hdr)
	}

	// vuka gen -inplace writes the file, for plain go build.
	if err := gen([]string{"-inplace"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if gen, err := os.ReadFile(filepath.Join(root, twGoName)); err != nil || !bytes.Contains(gen, []byte(".opacity-35")) {
		t.Errorf("vuka gen -inplace: %s: %v", twGoName, err)
	}
	if out, err := exec.Command("go", "build", "-o", app, ".").CombinedOutput(); err != nil {
		t.Errorf("go build after vuka gen -inplace: %v\n%s", err, out)
	}

	// Without the Tailwind CLI vuka build fails, and says how to install it.
	bin := t.TempDir()
	goBin, _ := exec.LookPath("go")
	os.Symlink(goBin, filepath.Join(bin, "go"))
	t.Setenv("PATH", bin)
	if err := runGo("build", []string{"-o", app, "."}, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), twInstall) {
		t.Errorf("vuka build without Tailwind: %v", err)
	}
	t.Setenv(twEnv, "off")
	if err := runGo("build", []string{"-o", app, "."}, io.Discard, &stderr); err != nil {
		t.Errorf("vuka build with VUKA_TAILWIND=off: %v\n%s", err, stderr.Bytes())
	}
}

// TestBuildTailwindOldWeb: an app on a web without RegisterCSS builds as
// before, with or without the Tailwind CLI.
func TestBuildTailwindOldWeb(t *testing.T) {
	bin := t.TempDir()
	goBin, _ := exec.LookPath("go")
	os.Symlink(goBin, filepath.Join(bin, "go"))
	t.Setenv("PATH", bin)
	t.Setenv("GOWORK", "off")
	root := tempModule(t, map[string]string{
		"go.mod":     "module example.com/app\n\ngo 1.25.0\n\nrequire github.com/vuka-lang/web v0.4.0\n\nreplace github.com/vuka-lang/web => ./web\n",
		"main.go":    "package main\n\nimport _ \"github.com/vuka-lang/web\"\n\nfunc main() {}\n",
		"page.html":  "<p class=\"p-4\"></p>",
		"web/go.mod": "module github.com/vuka-lang/web\n\ngo 1.25.0\n",
		"web/web.go": "package web\n",
	})
	gens, err := buildTailwind(root, "", nil, io.Discard)
	if err != nil || gens != nil {
		t.Errorf("buildTailwind: %v %v", gens, err)
	}
	writeFile(t, filepath.Join(root, "web", "css.go"), "package web\n\nfunc RegisterCSS(name string, css []byte) {}\n")
	if _, err := buildTailwind(root, "", nil, io.Discard); err == nil || !strings.Contains(err.Error(), twInstall) {
		t.Errorf("a web with RegisterCSS, no Tailwind CLI: %v", err)
	}
}
