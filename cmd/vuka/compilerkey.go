package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
)

// compilerVersion names this vuka for the transpile cache.
func compilerVersion() string {
	bi, _ := debug.ReadBuildInfo()
	return compilerKey(bi, executableID)
}

// compilerKey is what the transpile cache keys this vuka by. A build whose
// source the build info names — the command and every module it links at
// versions with checksums, built by the Go toolchain it names — is that
// source: another build of it transpiles the same, so it shares the cache
// (go install of a release or of main, rebuilt at will). A build made of
// local source — a workspace or directory replace, a dirty checkout, no build
// info — names nothing, and is its executable: version, path, size and
// modification time, so a rebuilt one never reads what another wrote.
func compilerKey(bi *debug.BuildInfo, executable func() string) string {
	if id := sourceID(bi); id != "" {
		return version + " build " + id
	}
	return version + executable()
}

// sourceID is a hash of bi's source, "" when bi doesn't name one.
func sourceID(bi *debug.BuildInfo) string {
	if bi == nil || bi.GoVersion == "" {
		return ""
	}
	var lines []string
	module := func(m *debug.Module) bool {
		if m.Replace != nil {
			m = m.Replace
		}
		if validVersion(m.Version) == "" || strings.Contains(m.Version, "+dirty") || m.Sum == "" {
			return false
		}
		lines = append(lines, m.Path+" "+m.Version+" "+m.Sum)
		return true
	}
	main := bi.Main
	main.Sum = "main" // a main module has no checksum: its version is a commit's
	if main.Path == "" || !module(&main) {
		return ""
	}
	for _, d := range bi.Deps {
		if !module(d) {
			return ""
		}
	}
	slices.Sort(lines)
	h := sha256.Sum256([]byte(bi.GoVersion + "\n" + strings.Join(lines, "\n")))
	return fmt.Sprintf("%x", h[:12])
}

// executableID names the running executable: where it is and when it was
// built.
func executableID() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	// Through a link (the go shim's, a gopls link) the one file: macOS's
	// os.Executable names the link, which would key a cache of its own.
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	fi, err := os.Stat(exe)
	if err != nil {
		return ""
	}
	return fmt.Sprintf(" %s %d %d", exe, fi.Size(), fi.ModTime().UnixNano())
}
