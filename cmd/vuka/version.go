package main

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/vuka-lang/vuka/internal/load"
)

// versionCmd prints this command's toolchain set and, inside an app, the
// set's versions its go.mod requires, warning when they aren't one set or
// not the set this command expects.
func versionCmd(args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: vuka version")
	}
	exp := expectedSet()
	if v, ok := oneSet(exp); ok && v == version {
		fmt.Fprintf(stdout, "vuka %s (vuka, ui, web %s)\n", version, v)
	} else {
		fmt.Fprintf(stdout, "vuka %s (vuka %s, ui %s, web %s)\n", version, vukaVersion, uiVersion, webVersion)
	}
	root, modPath, err := load.ModuleRoot(".")
	if err != nil || modPath == vukaModPath || modPath == cmdModPath || slices.Contains(setModules, modPath) {
		return nil
	}
	m, err := readGoMod(root)
	if err != nil {
		return nil
	}
	have := m.setVersions()
	if len(have) == 0 {
		return nil
	}
	fmt.Fprintf(stdout, "\n%s:\n", modPath)
	for _, p := range setModules {
		v, ok := have[p]
		if !ok {
			continue
		}
		line := fmt.Sprintf("  %-26s %s", p, v)
		if r := m.replaced(p); r != "" {
			line += " => " + r
		}
		fmt.Fprintln(stdout, line)
	}
	matches := true
	for p, v := range have {
		matches = matches && exp[p] == v
	}
	if v, ok := oneSet(have); !ok && !matches {
		var parts []string
		for _, p := range setModules {
			if v, ok := have[p]; ok {
				parts = append(parts, shortModule(p)+" "+v)
			}
		}
		fmt.Fprintf(stderr, "warning: %s is not one vuka set (%s); vuka upgrade moves it to one\n", modPath, strings.Join(parts, ", "))
	} else if want, ok := oneSet(exp); ok && v != want {
		fmt.Fprintf(stderr, "warning: %s uses the %s set, this vuka is %s; vuka upgrade %s moves it\n", modPath, v, want, want)
	}
	return nil
}
