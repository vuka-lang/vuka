package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/vuka-lang/vuka/transpile"
)

const helloVuka = `package main

import (
	"errors"
	"fmt"
	"os"
)

func greeting(name string) Result[string] {
	if name == "" {
		return Err(errors.New("who should I greet?"))
	}
	return Ok("Hello, " + name + "!")
}

func main() {
	name := "Vuka"
	if len(os.Args) > 1 {
		name = os.Args[1]
	}
	match greeting(name) {
	case Ok(text):
		fmt.Println(text)
	case Err(e):
		fmt.Println("error:", e)
	}
}
`

// newProject creates a module with a main.vuka and requires the runtime.
func newProject(args []string, stdout io.Writer) error {
	if len(args) < 1 || len(args) > 2 {
		return errors.New("usage: vuka new <dir> [module path]")
	}
	dir := args[0]
	module := filepath.Base(dir)
	if len(args) == 2 {
		module = args[1]
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s exists and isn't empty", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	run := func(args ...string) error {
		c := exec.Command("go", args...)
		c.Dir = dir
		out, err := c.CombinedOutput()
		if err != nil {
			return fmt.Errorf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return nil
	}
	if err := run("mod", "init", module); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "main.vuka"), []byte(helloVuka), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("/build/\n"), 0o644); err != nil {
		return err
	}
	if err := run("get", transpile.RuntimePath+"@latest"); err != nil {
		fmt.Fprintf(stdout, "warning: couldn't add the runtime yet (%v); run vuka mod tidy when online\n", err)
	}
	fmt.Fprintf(stdout, "created %s (module %s)\n\n\tcd %s\n\tvuka run .\n", dir, module, dir)
	return nil
}
