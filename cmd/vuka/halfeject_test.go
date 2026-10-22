package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// halfModule is a module of files requiring ui (and, with orm, the orm),
// the current directory for the test.
func halfModule(t *testing.T, files map[string]string, orm bool) string {
	t.Helper()
	if testing.Short() {
		t.Skip("runs the go command")
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	files["go.mod"], files["go.sum"] = testModule(t, "example.com/app")
	for name, src := range files {
		writeFile(t, filepath.Join(root, name), src)
	}
	if orm {
		dir := os.Getenv("VUKA_ORM")
		if dir == "" {
			repo, _ := filepath.Abs("../..")
			dir = filepath.Join(filepath.Dir(repo), "vuka-orm")
		}
		if src, _ := os.ReadFile(filepath.Join(dir, "vuka_base.go")); !bytes.Contains(src, []byte("type Base[")) {
			t.Skipf("github.com/vuka-lang/orm/v2 at %s has no Base; set VUKA_ORM", dir)
		}
		withModule(t, root, "github.com/vuka-lang/orm/v2", dir)
	}
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(root)
	return root
}

// buildRun builds the main package with vuka build and runs it.
func buildRun(t *testing.T, what string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "app")
	var stderr bytes.Buffer
	if err := runGo("build", []string{"-o", bin, "."}, io.Discard, &stderr); err != nil {
		t.Fatalf("vuka build %s: %v\n%s", what, err, stderr.Bytes())
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", what, err, out)
	}
	return string(out)
}

const unexportedModel = `package main

import (
	"fmt"

	"github.com/vuka-lang/orm/v2"
)

type userRow struct {
	orm.Base
	ID       int64
	Username string
}

func main() {
	fmt.Println(userRow.Username.Eq("ada") != nil)
}
`

// TestBuildUnexportedModel builds a package whose unexported model's typed
// fields (userRowFields) only orm generate writes: vuka build overlays them
// as vuka eject writes them.
func TestBuildUnexportedModel(t *testing.T) {
	halfModule(t, map[string]string{"main.vuka": unexportedModel}, true)
	if out := buildRun(t, "an unexported model"); out != "true\n" {
		t.Errorf("built, it prints %q", out)
	}
	if err := eject([]string{"-rm", "."}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("go", "build", "-o", os.DevNull, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build after vuka eject -rm: %v\n%s", err, out)
	}
}

// halfEjected is a module with a Go package (entities) between two Vuka
// ones: main imports it, and it imports mirror.
var halfEjected = map[string]string{
	"main.vuka": `package main

import (
	"fmt"

	"example.com/app/entities"
)

func greet() error {
	s := entities.Hello()?
	fmt.Println(s)
	return nil
}

func main() {
	if err := greet(); err != nil {
		panic(err)
	}
}
`,
	"entities/entities.go": `package entities

import "example.com/app/mirror"

func Hello() (string, error) { return "hello " + mirror.Name(), nil }
`,
	"mirror/mirror.vuka": `package mirror

func Name() string { return "mirror" }
`,
}

// TestBuildHalfEjected builds a module ejected in part: a Go package
// importing a Vuka package is type-checked with that package's Go.
func TestBuildHalfEjected(t *testing.T) {
	files := map[string]string{}
	for k, v := range halfEjected {
		files[k] = v
	}
	halfModule(t, files, false)
	if out := buildRun(t, "a half-ejected module"); out != "hello mirror\n" {
		t.Errorf("built, it prints %q", out)
	}
	if err := eject([]string{"-rm", "./entities", "."}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if out := buildRun(t, "main ejected too"); out != "hello mirror\n" {
		t.Errorf("built, it prints %q", out)
	}
}

// TestEjectModels ejects a package of models other packages query, which
// itself imports a Vuka package: the models' generated code is written
// with it, whether or not the package reads its own typed fields.
func TestEjectModels(t *testing.T) {
	halfModule(t, map[string]string{
		"main.vuka": `package main

import (
	"fmt"

	"example.com/app/entities"
)

func main() { fmt.Println(entities.Applicant.Name.Eq("ada") != nil, entities.Label()) }
`,
		"entities/applicant.vuka": `package entities

import (
	"example.com/app/mirror"
	"github.com/vuka-lang/orm/v2"
)

type Applicant struct {
	orm.Base
	ID   int64
	Name string
}

func Label() string { return mirror.Name() }
`,
		"mirror/mirror.vuka": `package mirror

func Name() string { return "mirror" }
`,
	}, true)
	var out bytes.Buffer
	if err := eject([]string{"-rm", "./entities"}, &out); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"applicant.go", "applicant_gen.go", "orm_scanners_gen.go"} {
		if _, err := os.Stat(filepath.Join("entities", name)); err != nil {
			t.Errorf("vuka eject -rm ./entities: %v\n%s", err, out.String())
		}
	}
	if got := buildRun(t, "entities ejected"); got != "true mirror\n" {
		t.Errorf("built, it prints %q", got)
	}
}

// TestEjectModelsFailure ejects a package whose models' code can't be
// generated: eject says so, naming the package.
func TestEjectModelsFailure(t *testing.T) {
	halfModule(t, map[string]string{
		"entities/applicant.vuka": `package entities

import "github.com/vuka-lang/orm/v2"

type Applicant struct {
	orm.Base
	ID   int64
	Name string
}
`,
		"entities/applicant_gen.go": "package entities\n\n// written by hand\n",
	}, true)
	err := eject([]string{"-rm", "./entities"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "applicant_gen.go") {
		t.Fatalf("vuka eject -rm with the typed fields' file in the way: %v", err)
	}
	if _, err := os.Stat("entities/applicant.vuka"); err != nil {
		t.Errorf("a failed eject removed the source: %v", err)
	}
}
