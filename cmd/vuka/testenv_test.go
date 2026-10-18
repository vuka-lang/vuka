package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// needs skips a test whose tool, checkout or node_modules is missing. Under
// CI (CI set) it fails instead: there a missing piece is a broken setup,
// never a pass.
func needs(t testing.TB, format string, args ...any) {
	t.Helper()
	msg := fmt.Sprintf(format, args...)
	if os.Getenv("CI") != "" {
		t.Fatalf("%s (CI is set, so this fails instead of skipping)", msg)
	}
	t.Skip(msg)
}

// fixtureModules is $env, else testdata/node/<name>/node_modules when it was
// installed there (npm ci --prefix testdata/node/<name>), else "".
func fixtureModules(env, name string) string {
	if nm := os.Getenv(env); nm != "" {
		return nm
	}
	nm, _ := filepath.Abs(filepath.Join("testdata", "node", name, "node_modules"))
	if _, err := os.Stat(nm); err == nil {
		return nm
	}
	return ""
}

// vitePlugin is $VUKA_PLUGIN, else vite/vuka.js in the web checkout.
func vitePlugin() string {
	if p := os.Getenv("VUKA_PLUGIN"); p != "" {
		return p
	}
	web := os.Getenv("VUKA_WEB")
	if web == "" {
		repo, _ := filepath.Abs("../..")
		web = filepath.Join(filepath.Dir(repo), "vuka-web")
	}
	p := filepath.Join(web, "vite", "vuka.js")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}
