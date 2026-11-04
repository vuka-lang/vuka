package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// goplsLoad is a package load in gopls's log: its number and its query.
var goplsLoad = regexp.MustCompile(`go/packages\.Load #(\d+)\b.*?query=\[([^\]]*)\]`)

// TestLSPStartup checks that gopls loads a module's generated files with
// its workspace, not each on its own (lsp_staging.go): a go list of a few
// hundred milliseconds per file in a real module. It logs the time to the
// first hover and to gopls settling.
func TestLSPStartup(t *testing.T) {
	logfile := filepath.Join(t.TempDir(), "gopls.log")
	// gopls writes its log messages, the loads among them, with -rpc.trace.
	opts := lspOptions{renameCommands: true, goplsArgs: []string{"-logfile", logfile, "-rpc.trace"}}
	start := time.Now()
	c, dir, _ := startLSPOpts(t, startupModule(24), opts)
	done := make(chan struct{})
	defer close(done)
	go func() { // nothing here waits for diagnostics
		for {
			select {
			case <-c.diags:
			case <-done:
				return
			}
		}
	}()
	hover := func(pkg string) {
		t.Helper()
		path := filepath.Join(dir, pkg, "card.vuka.html")
		src, _ := os.ReadFile(path)
		uri := pathToURI(path)
		c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka-html", "version": 1, "text": string(src)}})
		at := map[string]any{"textDocument": map[string]any{"uri": uri}, "position": positionOf(src, strings.Index(string(src), "Pets []"))}
		if h, _ := c.call("textDocument/hover", at).(map[string]any); !strings.Contains(fmt.Sprint(h["contents"]), "[]string") {
			t.Errorf("%s: hover %v\n%s", pkg, h, c.log.String())
		}
	}
	hover("p07")
	first := time.Since(start)

	loads := func() (all, files int) {
		b, _ := os.ReadFile(logfile)
		seen := map[string]bool{}
		for _, m := range goplsLoad.FindAllStringSubmatch(string(b), -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				all++
				if strings.Contains(m[2], "file=") {
					files++
				}
			}
		}
		return all, files
	}
	// gopls has settled once it has loaded nothing for a while.
	n, last := 0, time.Now()
	for time.Since(last) < 3*time.Second && time.Since(start) < time.Minute {
		time.Sleep(100 * time.Millisecond)
		if all, _ := loads(); all != n {
			n, last = all, time.Now()
		}
	}
	for _, pkg := range []string{"p03", "p12", "p19"} {
		hover(pkg)
	}
	all, files := loads()
	t.Logf("first hover after %v; gopls settled after %v, %d loads", first.Round(time.Millisecond), last.Sub(start).Round(time.Millisecond), all)
	if files > 0 {
		t.Errorf("gopls loaded %d generated files on their own; %d loads in all", files, all)
	}
}

// startupModule is a module of n packages, each a .vuka file and a
// .vuka.html component, every other one with a Go file too.
func startupModule(n int) map[string]string {
	files := map[string]string{
		"core/core.vuka": "package core\n\nimport \"fmt\"\n\nfunc Find(id int) Result[string] {\n\tif id == 0 {\n\t\treturn Err(fmt.Errorf(\"none\"))\n\t}\n\treturn Ok(fmt.Sprint(id))\n}\n",
	}
	var imports, uses strings.Builder
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("p%02d", i)
		files[name+"/model.vuka"] = fmt.Sprintf("package %s\n\nimport \"lsptest/core\"\n\nfunc Label(id int) Result[string] {\n\ts := core.Find(id)?\n\treturn Ok(\"#\" + s)\n}\n", name)
		files[name+"/card.vuka.html"] = fmt.Sprintf("package %s\n\n// Card lists pets.\ntype Card struct {\n\tPets []string\n\tID   int\n}\n\n<section>\n\t<h1>Pets ({len(Pets)})</h1>\n\tfor _, p := range Pets {\n\t\t<p>{p}</p>\n\t}\n</section>\n", name)
		if i%2 == 0 {
			files[name+"/doc.go"] = fmt.Sprintf("// Package %[1]s is one of the module's packages.\npackage %[1]s\n\nconst Name = %[1]q\n", name)
		}
		fmt.Fprintf(&imports, "\t\"lsptest/%s\"\n", name)
		fmt.Fprintf(&uses, "\t_ = %s.Card{}\n", name)
	}
	files["main.vuka"] = "package main\n\nimport (\n" + imports.String() + ")\n\nfunc main() {\n" + uses.String() + "}\n"
	return files
}
