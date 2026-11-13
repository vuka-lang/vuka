package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// openModule writes a module of n packages of files .vuka files each, p1
// importing p0, p2 importing p1 and so on, and a main importing them all.
func openModule(t *testing.T, n, files int) string {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	mod, sum := testModule(t, "lsptest")
	writeFile(t, filepath.Join(dir, "go.mod"), mod)
	writeFile(t, filepath.Join(dir, "go.sum"), sum)
	main := "package main\n\nimport (\n\t\"fmt\"\n\n"
	for i := range n {
		main += fmt.Sprintf("\t\"lsptest/p%d\"\n", i)
		for j := range files {
			src := fmt.Sprintf("package p%d\n\n", i)
			if i > 0 && j == 0 {
				src += fmt.Sprintf("import \"lsptest/p%d\"\n\nfunc Up() int { return p%d.F0() + 1 }\n\n", i-1, i-1)
			}
			src += fmt.Sprintf("func F%d() int { return %d }\n", j, j)
			writeFile(t, filepath.Join(dir, fmt.Sprintf("p%d", i), fmt.Sprintf("f%d.vuka", j)), src)
		}
	}
	main += ")\n\nfunc main() {\n"
	for i := range n {
		main += fmt.Sprintf("\tfmt.Println(p%d.F0())\n", i)
	}
	writeFile(t, filepath.Join(dir, "main.vuka"), main+"}\n")
	t.Setenv("GOFLAGS", "")
	return dir
}

// goplsLines is the mute gopls's log, without what it was told before
// skip lines.
func goplsLines(log string, skip int) []string {
	lines := strings.Split(strings.TrimSpace(logText(log)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	return lines[min(skip, len(lines)):]
}

// TestLSPOpensPackages: the first build sends gopls a module's generated
// files all at once, while gopls is staged, and then the workspace's
// folders, so gopls loads them together (lsp_staging.go). A build that
// changes nothing sends gopls nothing, and an edit sends the one file it
// changed.
func TestLSPOpensPackages(t *testing.T) {
	const n, files = 6, 10 // more files than openAtOnce
	log := muteGoplsLog(t)
	var plog syncBuffer
	app := openModule(t, n, files)
	c := startMutedLSPLog(t, app, &plog)
	waitFor(t, func() bool { return strings.Contains(logText(log), "workspace/didChangeWorkspaceFolders") })
	time.Sleep(100 * time.Millisecond)

	lines := goplsLines(log, 0)
	opened := map[string]bool{}
	for _, line := range lines {
		method, arg, _ := strings.Cut(line, " ")
		if method == "workspace/didChangeWorkspaceFolders" {
			break // what follows is the staging folder's (unstage)
		}
		switch {
		case method == "textDocument/didOpen":
			if opened[arg] {
				t.Errorf("%s opened twice", arg)
			}
			opened[arg] = true
		case line == "workspace/executeCommand gopls.list_imports":
			t.Errorf("gopls was asked to load a package of the first build")
		case method == "textDocument/didChange" || method == "textDocument/didClose":
			t.Errorf("first build: %s", line)
		}
	}
	if len(opened) < n*files+1 {
		t.Errorf("gopls was sent %d generated files, want at least %d", len(opened), n*files+1)
	}

	// Packages added later reach gopls a package at a time, in dependency
	// order, gopls asked once a package to load it before the next: the
	// loads scale with the packages, not the files.
	before := len(goplsLines(log, 0))
	for i := range n {
		for j := range files {
			src := fmt.Sprintf("package q%d\n\n", i)
			if i > 0 && j == 0 {
				src += fmt.Sprintf("import \"lsptest/q%d\"\n\nfunc Up() int { return q%d.F0() + 1 }\n\n", i-1, i-1)
			}
			writeFile(t, filepath.Join(app, fmt.Sprintf("q%d", i), fmt.Sprintf("f%d.vuka", j)), src+fmt.Sprintf("func F%d() int { return %d }\n", j, j))
		}
	}
	c.notify("workspace/didChangeWatchedFiles", map[string]any{"changes": []any{map[string]any{"uri": pathToURI(filepath.Join(app, "q0", "f0.vuka")), "type": 1}}})
	waitFor(t, func() bool { return strings.Count(logText(log), "textDocument/didOpen") > 2*n*files })
	time.Sleep(100 * time.Millisecond)
	lines = goplsLines(log, before)
	var runs []string // the package directory of each run of didOpens
	asks := 0
	for i, line := range lines {
		method, arg, _ := strings.Cut(line, " ")
		switch {
		case method == "textDocument/didOpen":
			if dir := filepath.Dir(uriToPath(arg)); len(runs) == 0 || runs[len(runs)-1] != dir {
				if slices.Contains(runs, dir) {
					t.Errorf("%s: the package's files were sent in two runs:\n%s", dir, strings.Join(lines, "\n"))
				}
				if len(runs) > 0 && (i == 0 || lines[i-1] != "workspace/executeCommand gopls.list_imports") {
					t.Errorf("%s was sent before gopls had loaded %s", dir, runs[len(runs)-1])
				}
				runs = append(runs, dir)
			}
		case line == "workspace/executeCommand gopls.list_imports":
			asks++
		}
	}
	if len(runs) != n || asks != n-1 {
		t.Errorf("new packages sent in %d runs, gopls asked to load %d times; want %d runs and %d loads:\n%s", len(runs), asks, n, n-1, strings.Join(lines, "\n"))
	}
	for i := 1; i < n; i++ {
		if slices.Index(runs, filepath.Join(app, fmt.Sprintf("q%d", i))) < slices.Index(runs, filepath.Join(app, fmt.Sprintf("q%d", i-1))) {
			t.Errorf("q%d was sent before what it imports: %v", i, runs)
		}
	}

	// The editor opens a file and types what it already holds: the builds
	// send gopls nothing.
	path := filepath.Join(app, "p3", "f1.vuka")
	src, _ := os.ReadFile(path)
	uri := pathToURI(path)
	builds := func() int { return strings.Count(plog.String(), "built the module") }
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka", "version": 1, "text": string(src)}})
	waitFor(t, func() bool { return builds() >= 3 })
	before = len(goplsLines(log, 0))
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": string(src)}}})
	waitFor(t, func() bool { return builds() >= 4 })
	time.Sleep(100 * time.Millisecond)
	if got := goplsLines(log, before); len(got) > 0 {
		t.Errorf("a build that changed nothing sent gopls:\n%s", strings.Join(got, "\n"))
	}

	// An edit of one function's body: one didChange, of its file.
	edited := strings.Replace(string(src), "return 1", "return 2", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 3},
		"contentChanges": []any{map[string]any{"text": edited}}})
	waitFor(t, func() bool { return builds() >= 5 })
	time.Sleep(100 * time.Millisecond)
	want := "textDocument/didChange " + pathToURI(filepath.Join(app, "p3", "f1_vuka.go"))
	if got := goplsLines(log, before); len(got) != 1 || got[0] != want {
		t.Errorf("an edit of one file sent gopls:\n%s\nwant\n%s", strings.Join(got, "\n"), want)
	}
}
