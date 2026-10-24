package load

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// withGoVersions makes vuka's Go vuka and every module's module for the test.
func withGoVersions(t *testing.T, vuka, module string) {
	t.Helper()
	oldVuka, oldModule := vukaGo, moduleGo
	t.Cleanup(func() {
		vukaGo, moduleGo = oldVuka, oldModule
		goVersions.Clear()
	})
	vukaGo = func() string { return vuka }
	moduleGo = func(string) (string, error) { return module, nil }
	goVersions.Clear()
}

// newerExportData writes export data of the errors package claiming a
// unified IR version no Go reads yet, and returns its file.
func newerExportData(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "list", "-export", "-f", "{{.Export}}", "errors").Output()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatal(err)
	}
	i := bytes.Index(data, []byte("\n$$B\nu"))
	if i < 0 {
		t.Fatal("no unified export data")
	}
	binary.LittleEndian.PutUint32(data[i+len("\n$$B\nu"):], 1000)
	file := filepath.Join(t.TempDir(), "errors.a")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

// TestImporterNewerExportData: export data newer than the importer reads
// is an error naming both Gos and the reinstall command, not a panic.
func TestImporterNewerExportData(t *testing.T) {
	file := newerExportData(t)
	withGoVersions(t, "go1.25.3", "go1.27.1")
	im := NewImporter(t.TempDir(), "")
	im.exports["errors"] = file
	_, err := im.Import("errors")
	var e *GoVersionError
	if !errors.As(err, &e) {
		t.Fatalf("want a *GoVersionError, got %v", err)
	}
	msg := err.Error()
	for _, want := range []string{"cannot read the export data of errors", "version 1000", "go1.25.3", "go1.27.1",
		"GOTOOLCHAIN=go1.27.1", "go install github.com/vuka-lang/vuka/cmd/vuka/v2@", "go install github.com/vuka-lang/vuka/cmd/vukavet/v2@"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error lacks %q: %s", want, msg)
		}
	}
	if im.Unreadable() == nil {
		t.Error("the importer doesn't remember the unreadable export data")
	}
}

// TestImporterUndecodable: export data the importer can't read when the
// module's Go is vuka's is still an error.
func TestImporterUndecodable(t *testing.T) {
	file := newerExportData(t)
	withGoVersions(t, "go1.27.1", "go1.27.1")
	im := NewImporter(t.TempDir(), "")
	im.exports["errors"] = file
	_, err := im.Import("errors")
	var e *GoVersionError
	if err == nil || errors.As(err, &e) || !strings.Contains(err.Error(), "version 1000") {
		t.Fatalf("want a plain error, got %v", err)
	}
	if im.Unreadable() != nil {
		t.Error("unreadable export data recorded for a module of vuka's Go")
	}
}

func TestCheckGoVersions(t *testing.T) {
	for _, c := range []struct {
		vuka, module string
		newer        bool
	}{
		{"go1.25.3", "go1.27.1", true},
		{"go1.27.1", "go1.27.2", true}, // go1.27.2 writes export data go1.27.1 can't read
		{"go1.27.1 X:jsonv2", "go1.27.2", true},
		{"go1.27.1", "go1.27.1", false},
		{"go1.27.2", "go1.26.0", false},
		{"go1.27.1", "go1.27rc1", false},
		{"devel go1.28-abcdef", "go1.27.1", false},
		{"go1.27.1", "", false},
	} {
		err := CheckGoVersions(c.vuka, c.module)
		if (err != nil) != c.newer {
			t.Errorf("CheckGoVersions(%q, %q) = %v", c.vuka, c.module, err)
		}
	}
}

// TestTranspileGoVersion: a module built by a newer Go than vuka's stops
// before anything is transpiled, with or without a session.
func TestTranspileGoVersion(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.23\n")
	write(t, filepath.Join(root, "main.vuka"), "package main\n\nfunc main() {}\n")
	pkgs, err := Discover(root, "example.com/app", root, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	withGoVersions(t, "go1.25.3", "go1.27.1")
	var e *GoVersionError
	if _, _, err := Transpile(pkgs, t.TempDir(), Options{}); !errors.As(err, &e) || e.Vuka != "go1.25.3" || e.Module != "go1.27.1" {
		t.Errorf("Transpile: want a *GoVersionError, got %v", err)
	}
	sess, err := NewSession(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := sess.Transpile(pkgs, t.TempDir(), Options{}); !errors.As(err, &e) {
		t.Errorf("Session.Transpile: want a *GoVersionError, got %v", err)
	}

	withGoVersions(t, "go1.27.1", "go1.27.1")
	if _, _, err := Transpile(pkgs, t.TempDir(), Options{}); err != nil {
		t.Errorf("Transpile with vuka's Go: %v", err)
	}
}

// TestModuleGoVersionCached: the go command is asked once for a module, and
// again once its go.mod changes.
func TestModuleGoVersionCached(t *testing.T) {
	withGoVersions(t, "go1.27.1", "go1.27.1")
	calls := 0
	moduleGo = func(string) (string, error) { calls++; return "go1.27.1", nil }
	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.23\n")
	for range 3 {
		if v, _ := ModuleGoVersion(root); v != "go1.27.1" {
			t.Fatalf("ModuleGoVersion = %q", v)
		}
	}
	if calls != 1 {
		t.Errorf("go env ran %d times", calls)
	}
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.23\n\ntoolchain go1.27.1\n")
	ModuleGoVersion(root)
	if calls != 2 {
		t.Errorf("go env ran %d times after go.mod changed", calls)
	}
}
