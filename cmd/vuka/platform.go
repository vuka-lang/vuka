package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// What differs between the systems vuka runs on, besides how it starts and
// stops processes (proc_*.go).

// exeSuffix is what the system's executables end in: ".exe" on Windows,
// where a file without it doesn't run.
func exeSuffix() string { return goosExeSuffix(runtime.GOOS) }

func goosExeSuffix(goos string) string {
	if goos == "windows" {
		return ".exe"
	}
	return ""
}

// exe is the path a binary vuka builds and runs is written to: path with
// the system's suffix.
func exe(path string) string { return path + exeSuffix() }

// nodeBin is the command dir/node_modules/.bin has for name: on Windows the
// package managers' name.cmd (npm, pnpm, yarn) or name.exe (bun) shim, as
// the extensionless one is a shell script there; name elsewhere. When none
// is there it is the extensionless path, which doesn't exist.
func nodeBin(dir, name string) string {
	return goosNodeBin(runtime.GOOS, dir, name)
}

func goosNodeBin(goos, dir, name string) string {
	p := filepath.Join(dir, "node_modules", ".bin", name)
	if goos == "windows" {
		for _, ext := range []string{".cmd", ".exe"} {
			if fi, err := os.Stat(p + ext); err == nil && !fi.IsDir() {
				return p + ext
			}
		}
	}
	return p
}

// removeAll removes path and what it holds. On Windows a file a process
// had open, or the binary of one that has just exited, can't be removed for
// a moment: it tries again for a while.
func removeAll(path string) error {
	err := os.RemoveAll(path)
	if runtime.GOOS != "windows" {
		return err
	}
	for wait := 10 * time.Millisecond; err != nil && wait < 2*time.Second; wait *= 2 {
		time.Sleep(wait)
		err = os.RemoveAll(path)
	}
	return err
}

// rename is os.Rename, on Windows tried again for a while: a rename onto
// a file another process has open (an app reading it, a virus scanner)
// fails there until it is closed.
func rename(from, to string) error {
	err := os.Rename(from, to)
	if runtime.GOOS != "windows" {
		return err
	}
	for wait := 10 * time.Millisecond; err != nil && wait < 2*time.Second; wait *= 2 {
		time.Sleep(wait)
		err = os.Rename(from, to)
	}
	return err
}

// standaloneTailwind is the name Tailwind's standalone CLI is released
// under for the system: tailwindcss-windows-x64.exe, tailwindcss-macos-arm64…
func standaloneTailwind(goos, goarch string) string {
	osName := map[string]string{"darwin": "macos"}[goos]
	if osName == "" {
		osName = goos
	}
	arch := map[string]string{"amd64": "x64"}[goarch]
	if arch == "" {
		arch = goarch
	}
	return "tailwindcss-" + osName + "-" + arch + goosExeSuffix(goos)
}

// tailwindInstallHint is the error saying Tailwind's CLI isn't there, with
// how to install it on the system.
func tailwindInstallHint(goos, goarch string) string {
	npm := "npm i -D @tailwindcss/cli (in the module or its frontend/)"
	standalone := "the standalone " + standaloneTailwind(goos, goarch) + " on PATH (https://github.com/tailwindlabs/tailwindcss/releases)"
	if goos == "darwin" {
		return "Tailwind isn't installed: brew install tailwindcss (or " + npm + ")"
	}
	return "Tailwind isn't installed: " + npm + ", or put " + standalone
}

// fileURIPath is the path of a file URI's OS path: on Windows a drive
// letter's path gets a leading slash (C:\x → /C:/x), as a file URI has.
func fileURIPath(goos, path string) string {
	if goos != "windows" {
		return filepath.ToSlash(path)
	}
	p := strings.ReplaceAll(path, `\`, "/")
	if len(p) >= 2 && p[1] == ':' {
		p = "/" + p
	}
	return p
}

// uriOSPath is fileURIPath's inverse, on a file URI's (decoded) path.
func uriOSPath(goos, p string) string {
	if goos != "windows" {
		return filepath.Clean(filepath.FromSlash(p))
	}
	if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.Clean(strings.ReplaceAll(p, "/", `\`))
}

// entryInfo is what a look at the sources sees of the file at path, which d
// is the directory entry of. Windows' directory listing can say what a file
// was before its last write (NTFS updates a file's directory entry lazily):
// the file itself is asked there.
func entryInfo(path string, d fs.DirEntry) (fs.FileInfo, error) {
	if runtime.GOOS == "windows" {
		return os.Lstat(path)
	}
	return d.Info()
}
