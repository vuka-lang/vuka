package main

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLocalCounter builds and runs the counter example with the local
// sandbox: RUNNER_LOCAL_TEST=1, with vuka on PATH and the docs' modules in
// the module cache.
func TestLocalCounter(t *testing.T) {
	if os.Getenv("RUNNER_LOCAL_TEST") != "1" {
		t.Skip("RUNNER_LOCAL_TEST=1 runs it")
	}
	vuka, err := exec.LookPath("vuka")
	if err != nil {
		t.Skip(err)
	}
	t.Setenv("RUNNER_UNSAFE_LOCAL", "1")
	sb, err := newLocal(vuka)
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := loadTemplate("../../docs/testdata/gomode/go.mod", absPath("../.."))
	if err != nil {
		t.Fatal(err)
	}
	s, err := newServer(config{runOrigin: "http://*.run.test", sessions: 2, idle: time.Minute, maxLife: time.Hour,
		buildTimeout: 2 * time.Minute, dir: t.TempDir()}, sb, tmpl)
	if err != nil {
		t.Fatal(err)
	}
	defer s.shutdown()
	files := example(t, "../../docs/playground/examples/counter")
	s.take()
	res, ss := s.run(t.Context(), files, nil, nil)
	if ss == nil {
		t.Fatalf("%+v", res)
	}
	hs := newTestHTTP(t, s)
	code, body, _ := get(t, hs, res.URL, "/")
	if code != 200 || !strings.Contains(body, "Clicked") {
		t.Fatalf("%d %s", code, body)
	}
	files["pages/index.vuka.html"] = strings.Replace(files["pages/index.vuka.html"], "p.count++", `p.count += "x"`, 1)
	s.take()
	res, ss = s.run(t.Context(), files, nil, nil)
	if ss != nil || len(res.Diagnostics) == 0 || res.Diagnostics[0].Span.File != "pages/index.vuka.html" {
		t.Fatalf("%+v", res)
	}
}

func example(t *testing.T, dir string) map[string]string {
	files := map[string]string{}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			b, _ := os.ReadFile(p)
			files[filepath.ToSlash(rel)] = string(b)
		}
		return err
	})
	return files
}
