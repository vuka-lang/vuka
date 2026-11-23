package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const shimUsage = `usage: vuka shim install|status|remove [-dir dir]

The go shim makes plain go build, go test, go vet and go run in a vuka app
see the module's .vuka files: they are transpiled into the build's
overlay. go test, go vet and go run are vuka test, vet and run. go build
is the overlay and the Tailwind stylesheet the app embeds: the binary
plain go build would make with the generated code on disk, with your
flags as given — none of vuka build's release flags (-ldflags=-w,
-tags=vuka_release), build/ module or frontend (vite) build; run vuka
build for those. Every other go command (go
mod, go env, go version, go list, go tool…), and every go command outside
a vuka app, runs the real go untouched.

	vuka shim install    put a go linked to this vuka in the shim directory,
	                     and print the PATH line that puts it first
	vuka shim status     the shim, what it runs, whether it is first on PATH,
	                     and the real go it passes the rest to
	vuka shim remove     delete it (and the directory, if left empty)

The shim directory is ~/.vuka/shims, or -dir, or $VUKA_SHIM_DIR. vuka never
edits a shell's startup file or the Windows registry: add the PATH line
yourself. The real go is $VUKA_REAL_GO, else the first go on PATH that
isn't a shim; VUKA_GO_SHIM=off passes every command to it.

The shim covers go's commands only: vuka new, fmt, lsp and the rest are
vuka's own. On CI and teammates' machines go build is the plain go unless
they install the shim too: run vuka build and vuka test there.
`

// shimCmd is vuka shim.
func shimCmd(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(stdout, shimUsage)
		if len(args) == 0 {
			return exitStatus(2)
		}
		return nil
	}
	sub := args[0]
	fs := flag.NewFlagSet("vuka shim "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, shimUsage) }
	dirFlag := fs.String("dir", "", "the shim directory (default $VUKA_SHIM_DIR, else ~/.vuka/shims)")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return exitStatus(2)
	}
	dir := *dirFlag
	if dir == "" {
		d, err := shimDir(os.Getenv)
		if err != nil {
			return err
		}
		dir = d
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(self); err == nil {
		self = real
	}
	switch sub {
	case "install":
		return shimInstall(dir, self, runtime.GOOS, hostLinker, stdout)
	case "status":
		return shimStatus(dir, self, runtime.GOOS, stdout)
	case "remove":
		return shimRemove(dir, runtime.GOOS, stdout)
	}
	fmt.Fprintf(stderr, "vuka shim: unknown command %q\n\n%s", sub, shimUsage)
	return exitStatus(2)
}

func isHelp(a string) bool { return a == "-h" || a == "-help" || a == "--help" || a == "help" }

// shimEntry is the shim's path in dir: go, go.exe on Windows.
func shimEntry(dir, goos string) string { return filepath.Join(dir, "go"+goosExeSuffix(goos)) }

// linker makes the shim's entry; the tests replace it to take the Windows
// fallbacks where links do work.
type linker struct {
	symlink func(target, entry string) error
	link    func(target, entry string) error
	copy    func(target, entry string) error
}

var hostLinker = linker{symlink: os.Symlink, link: os.Link, copy: copyExecutable}

// makeEntry makes entry run target: a symbolic link, which follows vuka
// when it is reinstalled; on Windows, where one needs Developer Mode or an
// administrator, a hard link next (the same volume only), then a copy.
// It returns which it made.
func makeEntry(goos, target, entry string, l linker) (string, error) {
	err := l.symlink(target, entry)
	if err == nil {
		return "symlink", nil
	}
	if goos != "windows" {
		return "", err
	}
	errs := []error{fmt.Errorf("symlink: %w", err)}
	if err := l.link(target, entry); err == nil {
		return "hard link", nil
	} else {
		errs = append(errs, fmt.Errorf("hard link: %w", err))
	}
	if err := l.copy(target, entry); err == nil {
		return "copy", nil
	} else {
		errs = append(errs, fmt.Errorf("copy: %w", err))
	}
	return "", errors.Join(errs...)
}

func copyExecutable(target, entry string) error {
	in, err := os.Open(target)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(entry, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(entry)
		return err
	}
	return out.Close()
}

// isOurEntry reports whether the file at entry is a go shim: a link to a
// vuka, or a vuka itself (a hard link or a copy), or a symlink to a vuka
// since removed. A real go there is not.
func isOurEntry(entry string) bool {
	if isVukaBinary(entry, shimEnv().selfPath) {
		return true
	}
	fi, err := os.Lstat(entry)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return false
	}
	_, err = os.Stat(entry)
	return errors.Is(err, os.ErrNotExist)
}

func shimInstall(dir, self, goos string, l linker, w io.Writer) error {
	entry := shimEntry(dir, goos)
	if _, err := os.Lstat(entry); err == nil {
		if !isOurEntry(entry) {
			return fmt.Errorf("%s is there and isn't a vuka: remove it first, or install into another -dir", entry)
		}
		if err := os.Remove(entry); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	how, err := makeEntry(goos, self, entry, l)
	if err != nil {
		return fmt.Errorf("vuka shim: no %s: %w", entry, err)
	}
	fmt.Fprintf(w, "%s: a %s to %s\n", entry, how, self)
	if how != "symlink" {
		fmt.Fprintln(w, "It doesn't follow vuka when it is reinstalled: run vuka shim install again after upgrading vuka.")
	}
	order := pathOrder(dir, os.Getenv("PATH"), goos, os.Getenv("PATHEXT"))
	switch {
	case !order.onPath:
		fmt.Fprintf(w, "\n%s isn't on PATH. Put it first:\n\n%s\n", dir, pathAdvice(dir, goos, os.Getenv("SHELL")))
	case order.before != "":
		fmt.Fprintf(w, "\n%s is on PATH after %s, whose go runs instead. Put it first:\n\n%s\n", dir, order.before, pathAdvice(dir, goos, os.Getenv("SHELL")))
	default:
		fmt.Fprintf(w, "%s is first on PATH: go build, test, vet and run in a vuka app are vuka's.\n", dir)
	}
	return nil
}

func shimStatus(dir, self, goos string, w io.Writer) error {
	entry := shimEntry(dir, goos)
	fi, err := os.Lstat(entry)
	if err != nil {
		fmt.Fprintf(w, "no go shim at %s (vuka shim install makes one)\n", entry)
	} else {
		switch target, lerr := os.Readlink(entry); {
		case fi.Mode()&os.ModeSymlink != 0 && lerr == nil:
			fmt.Fprintf(w, "go shim: %s, a symlink to %s\n", entry, target)
		case isOurEntry(entry):
			fmt.Fprintf(w, "go shim: %s, a vuka (a hard link or a copy)\n", entry)
		default:
			fmt.Fprintf(w, "%s is there but isn't a vuka\n", entry)
		}
		if isOurEntry(entry) && !sameInode(entry, self) {
			fmt.Fprintf(w, "  it runs another vuka than this one (%s): vuka shim install relinks it\n", self)
		}
	}
	order := pathOrder(dir, os.Getenv("PATH"), goos, os.Getenv("PATHEXT"))
	switch {
	case !order.onPath:
		fmt.Fprintf(w, "%s isn't on PATH: go is the real go\n", dir)
	case order.before != "":
		fmt.Fprintf(w, "%s is on PATH after %s, whose go runs instead\n", dir, order.before)
	default:
		fmt.Fprintf(w, "%s is first on PATH: go runs the shim\n", dir)
	}
	l := shimEnv()
	l.dirs = append(l.dirs, dir)
	if real, err := realGo(l, goos); err != nil {
		fmt.Fprintln(w, "real go:", err)
	} else {
		fmt.Fprintln(w, "real go:", real)
	}
	return nil
}

func shimRemove(dir, goos string, w io.Writer) error {
	entry := shimEntry(dir, goos)
	if _, err := os.Lstat(entry); err != nil {
		fmt.Fprintf(w, "no go shim at %s\n", entry)
		return nil
	}
	if !isOurEntry(entry) {
		return fmt.Errorf("%s isn't a vuka: left as it is", entry)
	}
	if err := os.Remove(entry); err != nil {
		return err
	}
	fmt.Fprintln(w, "removed", entry)
	if os.Remove(dir) == nil {
		fmt.Fprintln(w, "removed", dir)
	}
	return nil
}

// shimOrder is where the shim directory is on PATH: whether it is there
// at all, and the directory with a go before it, if any.
type shimOrder struct {
	onPath bool
	before string
}

func pathOrder(dir, path, goos, pathext string) shimOrder {
	var o shimOrder
	for _, d := range splitPathList(path, goos) {
		if d == "" {
			continue
		}
		if samePathDir(goos, d, dir) {
			o.onPath = true
			return o
		}
		if o.before != "" {
			continue
		}
		for _, name := range goNames(goos, pathext) {
			if fi, err := os.Stat(filepath.Join(d, name)); err == nil && !fi.IsDir() {
				o.before = d
				break
			}
		}
	}
	return o
}

// samePathDir reports whether PATH entry d is dir: the same directory, or
// the same path (ignoring case and trailing separators on Windows) when
// it doesn't exist.
func samePathDir(goos, d, dir string) bool {
	if sameInode(d, dir) {
		return true
	}
	clean := func(p string) string {
		p = strings.TrimRight(p, `/\`)
		if goos == "windows" {
			p = strings.ToLower(strings.ReplaceAll(p, "/", `\`))
		}
		return p
	}
	return clean(d) == clean(dir)
}

// pathAdvice is the line that puts dir first on PATH: PowerShell's on
// Windows, else the shell's ($SHELL: fish, else POSIX sh).
func pathAdvice(dir, goos, shell string) string {
	if goos == "windows" {
		return fmt.Sprintf(`	# this PowerShell session:
	$env:Path = "%[1]s;" + $env:Path
	# every new one (your user's PATH):
	[Environment]::SetEnvironmentVariable("Path", "%[1]s;" + [Environment]::GetEnvironmentVariable("Path", "User"), "User")`, dir)
	}
	home, _ := os.UserHomeDir()
	shown := dir
	if home != "" && strings.HasPrefix(dir, home+string(filepath.Separator)) {
		shown = "$HOME" + dir[len(home):]
	}
	switch filepath.Base(shell) {
	case "fish":
		return fmt.Sprintf("\tfish_add_path --move %s", strings.Replace(shown, "$HOME", "~", 1))
	case "zsh":
		return fmt.Sprintf("\texport PATH=\"%s:$PATH\"    # in ~/.zshrc", shown)
	case "bash":
		return fmt.Sprintf("\texport PATH=\"%s:$PATH\"    # in ~/.bashrc (~/.bash_profile on macOS)", shown)
	}
	return fmt.Sprintf("\texport PATH=\"%s:$PATH\"    # in your shell's startup file", shown)
}
