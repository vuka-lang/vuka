package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vuka-lang/vuka/internal/load"
)

const templSource = `package main

templ Hello(name string) {
	<p>Hello, { name }!</p>
}
`

const templVuka = `package main

import (
	"context"
	"os"
)

func main() {
	_ = Hello("vuka").Render(context.Background(), os.Stdout)
}
`

// templFiles is a module's go.mod (and go.sum) requiring templ as this repo does.
func templFiles(t *testing.T, files map[string]string) map[string]string {
	t.Helper()
	repo, _ := filepath.Abs("../..")
	mod, _ := os.ReadFile(filepath.Join(repo, "go.mod"))
	sum, _ := os.ReadFile(filepath.Join(repo, "go.sum"))
	version := ""
	for _, line := range strings.Split(string(mod), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == load.TemplPath {
			version = f[1]
		}
	}
	files["go.mod"] = "module lsptest\n\ngo 1.25.0\n\nrequire (\n\tgithub.com/vuka-lang/vuka v0.0.0\n\t" + load.TemplPath + " " + version +
		"\n)\n\nreplace github.com/vuka-lang/vuka => " + repo + "\n"
	files["go.sum"] = string(sum)
	return files
}

func TestModStubTempl(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module m\n\ngo 1.25.0\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "ui"), 0o755)
	os.WriteFile(filepath.Join(dir, "ui", "card.templ"), []byte("package ui\n\nimport \"strings\"\n\ntempl Card(s string) {\n\t<p>{ strings.ToUpper(s) }</p>\n}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "ui", "broken.templ"), []byte("package ui\n\ntempl Broken() {\n\t<p>\n}\n"), 0o644)
	pkgs, err := load.Discover(dir, "m", dir, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	stubs := importStubs(dir, pkgs)
	stub := string(stubs[filepath.Join(realDir(dir), "ui", stubName)])
	for _, want := range []string{"package ui", `_ "github.com/a-h/templ"`, `_ "github.com/a-h/templ/runtime"`, `_ "strings"`} {
		if !strings.Contains(stub, want) {
			t.Fatalf("stub lacks %s:\n%s\n(all: %v)", want, stub, stubs)
		}
	}
}

// TestLSPTempl: a .vuka file calling a templ component type-checks, its
// definition is in the .templ file, and a broken .templ file is reported on
// itself without upsetting the rest.
func TestLSPTempl(t *testing.T) {
	c, dir, _ := startLSPWith(t, templFiles(t, map[string]string{"main.vuka": templVuka, "hello.templ": templSource}))
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": templVuka}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })

	v := c.call("textDocument/definition", map[string]any{"textDocument": map[string]any{"uri": uri},
		"position": positionOf([]byte(templVuka), strings.Index(templVuka, "Hello(")+1)})
	b, _ := json.Marshal(v)
	if !strings.Contains(string(b), "hello.templ") || strings.Contains(string(b), "_templ.go") ||
		!strings.Contains(string(b), `"line":2`) {
		t.Fatalf("definition: %s, want line 2 of hello.templ", b)
	}

	templPath := filepath.Join(dir, "hello.templ")
	os.WriteFile(templPath, []byte("package main\n\ntempl Hello(name string) {\n\t<div>\n\t\t<span>open\n\t</div>\n}\n"), 0o644)
	c.notify("workspace/didChangeWatchedFiles", map[string]any{"changes": []any{map[string]any{"uri": pathToURI(templPath), "type": 2}}})
	ds := c.waitDiags(pathToURI(templPath), func(ds []any) bool { return len(ds) > 0 })
	if b, _ := json.Marshal(ds); !strings.Contains(string(b), `"source":"vuka"`) {
		t.Fatalf("diagnostics: %s", b)
	}

	os.WriteFile(templPath, []byte(templSource), 0o644)
	c.notify("workspace/didChangeWatchedFiles", map[string]any{"changes": []any{map[string]any{"uri": pathToURI(templPath), "type": 2}}})
	c.waitDiags(pathToURI(templPath), func(ds []any) bool { return len(ds) == 0 })
}

// TestTemplDropIn runs templ's own language server with vuka as its gopls (a
// link named gopls first on PATH): a .templ file sees the components of the
// .vuka files beside it, and templ's buffers win over vuka's Go for them.
func TestTemplDropIn(t *testing.T) {
	templBin, err := exec.LookPath("templ")
	if err != nil {
		t.Skip("templ not on PATH")
	}
	if _, err := findGopls(""); err != nil {
		t.Skip(err)
	}
	bin := t.TempDir()
	build := exec.Command("go", "build", "-o", filepath.Join(bin, "vuka"), ".")
	build.Env = append(os.Environ(), "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if err := os.Symlink(filepath.Join(bin, "vuka"), filepath.Join(bin, "gopls")); err != nil {
		t.Fatal(err)
	}
	pages := "package main\n\nfunc Nav(current string) vuka.Node {\n\treturn <nav>{current}</nav>\n}\n\nfunc main() { _ = Layout(\"home\") }\n"
	layout := "package main\n\ntempl Layout(current string) {\n\t<body>\n\t\t@Nav(current)\n\t</body>\n}\n"
	dir := realDir(t.TempDir())
	for name, src := range templFiles(t, map[string]string{"pages.vuka": pages, "layout.templ": layout}) {
		os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644)
	}

	cmd := exec.Command(templBin, "lsp", "-goplsLog", filepath.Join(bin, "gopls.log"), "-goplsRPCTrace")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "GOWORK=off",
		"VUKA_GOPLS_SHARED=0", "VUKA_LSP_LOG="+filepath.Join(bin, "vuka.log"))
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		in.Close()
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
			<-done
		}
	})
	c := &lspClient{t: t, conn: newRPCConn(out, in), resps: map[string]chan rpcMsg{}, diags: make(chan map[string]any, 100)}
	go c.loop()
	root := pathToURI(dir)
	c.call("initialize", map[string]any{"rootUri": root, "workspaceFolders": []any{map[string]any{"uri": root, "name": "t"}},
		"capabilities": map[string]any{}})
	c.notify("initialized", map[string]any{})
	uri := pathToURI(filepath.Join(dir, "layout.templ"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "templ", "version": 1, "text": layout}})

	at := map[string]any{"textDocument": map[string]any{"uri": uri},
		"position": positionOf([]byte(layout), strings.Index(layout, "Nav(")+1)}
	var hover string
	for i := 0; i < 50 && !strings.Contains(hover, "func Nav("); i++ {
		time.Sleep(200 * time.Millisecond)
		b, _ := json.Marshal(c.call("textDocument/hover", at))
		hover = string(b)
	}
	if !strings.Contains(hover, "func Nav(current string)") {
		log, _ := os.ReadFile(filepath.Join(bin, "vuka.log"))
		t.Fatalf("hover on Nav in layout.templ: %s\n%s", hover, log)
	}
	b, _ := json.Marshal(c.call("textDocument/definition", at))
	if !strings.Contains(string(b), pathToURI(filepath.Join(dir, "pages.vuka"))) || !strings.Contains(string(b), `"line":2`) {
		t.Fatalf("definition of Nav: %s, want line 2 of pages.vuka", b)
	}

	// Loaded now: an edit's diagnostics are gopls's on templ's buffer.
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": layout + "\n"}}})
	deadline := time.After(3 * time.Second)
	for {
		select {
		case p := <-c.diags:
			if b, _ := json.Marshal(p); strings.Contains(string(b), "undefined") {
				t.Fatalf("layout.templ should see Nav from pages.vuka: %s", b)
			}
			continue
		case <-deadline:
		}
		break
	}
}
