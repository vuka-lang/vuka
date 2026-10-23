package main

import (
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/diag"
)

// npm 10 (Node.js 20 and 22 ship it) crashes building a dependency tree
// from scratch for the scaffolds' package sets ("Cannot read properties of
// null (reading 'edgesOut')"); npm 11 resolves them, and npm 10 installs
// them from a lockfile (npm ci). checkNPM runs before every install.
const npmMinMajor = 11

const npmFix = "npm install -g npm@11 (Node.js 24 and newer ship it)"

// npmVersion is what npm --version prints, "" when it can't be run.
func npmVersion() string {
	out, err := exec.Command("npm", "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// npmMajor is the major version in npm --version's output: its last line
// that reads as one (a version, a leading v allowed); false when none does.
func npmMajor(out string) (int, bool) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		v := strings.TrimPrefix(strings.TrimSpace(lines[i]), "v")
		major, rest, _ := strings.Cut(v, ".")
		n, err := strconv.Atoi(major)
		if err == nil && n >= 0 && rest != "" {
			return n, true
		}
	}
	return 0, false
}

// npmTooOld reports whether npm --version's output is a version older than
// npmMinMajor; one that can't be read passes.
func npmTooOld(version string) bool {
	major, ok := npmMajor(version)
	return ok && major < npmMinMajor
}

// checkNPM is the gate before cmd installs the frontend at where: nothing for another package manager or npm 11+; for npm 10, a
// warning on w when cmd is npm ci (the lockfile installs), else an error.
func checkNPM(cmd []string, where string, w io.Writer) error {
	if len(cmd) == 0 || cmd[0] != "npm" {
		return nil
	}
	v := npmVersion()
	if !npmTooOld(v) {
		return nil
	}
	if cmd[len(cmd)-1] == "ci" {
		fmt.Fprintf(w, "warning: npm %s installs %s from its lockfile, but adding packages there needs npm %d or newer (npm 10 fails on this dependency set): %s\n",
			v, where, npmMinMajor, npmFix)
		return nil
	}
	return diag.New("E0610", "frontend: npm %s can't install %s: npm 10 fails on this dependency set (Cannot read properties of null (reading 'edgesOut')); it needs npm %d or newer", v, where, npmMinMajor).
		WithHelp("%s, then run the command again", npmFix)
}
