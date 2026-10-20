package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// The toolchain set. From firstSetVersion on, vuka, ui, web and this command
// are released together under one version, so all four constants are equal;
// vuka release writes them. vuka new pins the set, and vuka version and vuka
// upgrade compare an app's go.mod with it.
const (
	version     = "v0.50.0"
	vukaVersion = "v0.50.0"
	uiVersion   = "v0.50.0"
	webVersion  = "v0.50.0"
)

const firstSetVersion = "v0.50.0"

const (
	vukaModPath = "github.com/vuka-lang/vuka"
	uiModPath   = "github.com/vuka-lang/ui"
	webModPath  = "github.com/vuka-lang/web"
	cmdModPath  = "github.com/vuka-lang/vuka/cmd/vuka"
)

// setModules are the libraries of the set, in dependency order.
var setModules = []string{vukaModPath, uiModPath, webModPath}

// expectedSet is the version of each library this command expects.
func expectedSet() map[string]string {
	return map[string]string{vukaModPath: vukaVersion, uiModPath: uiVersion, webModPath: webVersion}
}

// goMod is what go mod edit -json reports of a go.mod.
type goMod struct {
	Module  struct{ Path string }
	Require []struct {
		Path     string
		Version  string
		Indirect bool
	}
	Replace []struct {
		Old struct{ Path, Version string }
		New struct{ Path, Version string }
	}
}

func readGoMod(dir string) (*goMod, error) {
	c := exec.Command("go", "mod", "edit", "-json")
	c.Dir = dir
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("go mod edit -json: %v\n%s", err, stderr.Bytes())
	}
	var m goMod
	if err := json.Unmarshal(out, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// setVersions are the set's libraries the module requires, by path.
func (m *goMod) setVersions() map[string]string {
	out := map[string]string{}
	for _, r := range m.Require {
		for _, p := range setModules {
			if r.Path == p {
				out[p] = r.Version
			}
		}
	}
	return out
}

// requires is the version of module p the module requires, or "".
func (m *goMod) requires(p string) string {
	for _, r := range m.Require {
		if r.Path == p {
			return r.Version
		}
	}
	return ""
}

// replaced is where a replace directive points module p, or "".
func (m *goMod) replaced(p string) string {
	for _, r := range m.Replace {
		if r.Old.Path == p {
			if r.New.Version != "" {
				return r.New.Path + " " + r.New.Version
			}
			return r.New.Path
		}
	}
	return ""
}

// oneSet reports whether the versions are one set: all the same, and a set
// version (from firstSetVersion on).
func oneSet(vs map[string]string) (string, bool) {
	v := ""
	for _, p := range setModules {
		if have, ok := vs[p]; ok {
			if v != "" && have != v {
				return "", false
			}
			v = have
		}
	}
	return v, v != "" && compareVersions(v, firstSetVersion) >= 0
}

func shortModule(p string) string {
	return strings.TrimPrefix(p, "github.com/vuka-lang/")
}
