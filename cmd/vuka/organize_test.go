package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// noVars is knowing the file reads no context processor's variable.
func noVars() map[string]bool { return map[string]bool{} }

// orgModule writes a module example.com/shop of files, requiring the
// toolchain's modules, and returns its directory.
func orgModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/shop\n\ngo 1.25.0\n\nrequire (\n\tgithub.com/vuka-lang/orm/v2 v2.8.0\n\tgithub.com/vuka-lang/ui/v2 v2.8.0\n\tgithub.com/vuka-lang/web/v2 v2.8.0\n)\n")
	for name, src := range files {
		writeFile(t, filepath.Join(dir, name), src)
	}
	return dir
}

// organizedText is the file at path with its imports organized.
func organizedText(t *testing.T, path string) (string, organized) {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	o, err := organizeImports(path, src, os.ReadFile, noVars)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if o.edit == nil {
		return string(src), o
	}
	return string(src[:o.edit.start]) + o.edit.text + string(src[o.edit.end:]), o
}

// organizesTo checks the file name of the module dir organizes to want,
// and that organizing that changes nothing.
func organizesTo(t *testing.T, dir, name, want string) organized {
	t.Helper()
	path := filepath.Join(dir, name)
	got, o := organizedText(t, path)
	if got != want {
		t.Fatalf("%s organized:\n%s\nwant:\n%s", name, got, want)
	}
	writeFile(t, path, got)
	if again, o2 := organizedText(t, path); again != got || o2.edit != nil {
		t.Errorf("%s organized twice:\n%s", name, again)
	}
	return o
}

func TestOrganizeImports(t *testing.T) {
	dir := orgModule(t, map[string]string{
		"components/logo.vuka.html": "<script lang=\"go\">\npackage components\n\ntype Logo struct{}\n</script>\n\n<img src=\"/logo.svg\"/>\n",
		"layouts/main.vuka.html":    "<script lang=\"go\">\npackage layouts\n\nimport \"github.com/vuka-lang/ui/v2\"\n\ntype Applicant struct{ Children ui.Node }\n</script>\n\n<main>{Children}</main>\n",
		"store/store.vuka": `package store

import (
	"fmt"
	"os"
)

func Name(s string) string {
	return strings.ToUpper(s) + filepath.Ext(s)
}
`,
		"store/rand.vuka": `package store

func Pick() int { return rand.Intn(3) }
`,
		"store/sorted.vuka": `package store

import (
	"example.com/shop/components"
	"github.com/vuka-lang/ui/v2"
	// errors are the package's own.
	"errors"
	"context" // for the deadline
	_ "embed"
)

var _ = errors.New
var _ context.Context
var _ ui.Node
var _ components.Logo
`,
		"store/broken.vuka": `package store

import (
	"strings"
	"time"
)

var n int = "not a number"

func Upper(s string) string { return strings.ToUpper(s) }

func Wait() time.Duration { return Missing(time.Second) }
`,
		"entities/user.vuka": `package entities

import (
	"fmt"
	"github.com/vuka-lang/orm/v2"
)

type User struct {
	ID   int64  @orm.PK
	Name string @orm.Char{}
}
`,
		"entities/post.vuka": `package entities

type Post struct {
	ID int64 @orm.PK
}
`,
		"pages/home.vuka.html": `<script lang="go">
package pages

import (
	"fmt"
	"strings"
)

type Home struct {
	Name string
}
</script>

<layouts.Applicant>
  <components.Logo/>
  <p>{strings.ToUpper(Name)} visits example.com</p>
</layouts.Applicant>
`,
	})

	organizesTo(t, dir, "store/store.vuka", `package store

import (
	"path/filepath"
	"strings"
)

func Name(s string) string {
	return strings.ToUpper(s) + filepath.Ext(s)
}
`)

	o := organizesTo(t, dir, "store/rand.vuka", `package store

func Pick() int { return rand.Intn(3) }
`)
	if got := o.ambiguous["rand"]; !reflect.DeepEqual(got, []string{"crypto/rand", "math/rand", "math/rand/v2"}) {
		t.Errorf("rand can mean %v", got)
	}

	organizesTo(t, dir, "store/sorted.vuka", `package store

import (
	"context" // for the deadline
	_ "embed"
	// errors are the package's own.
	"errors"

	"github.com/vuka-lang/ui/v2"

	"example.com/shop/components"
)

var _ = errors.New
var _ context.Context
var _ ui.Node
var _ components.Logo
`)

	broken := readFile(t, filepath.Join(dir, "store/broken.vuka"))
	organizesTo(t, dir, "store/broken.vuka", broken)

	organizesTo(t, dir, "entities/user.vuka", `package entities

import "github.com/vuka-lang/orm/v2"

type User struct {
	ID   int64  @orm.PK
	Name string @orm.Char{}
}
`)
	organizesTo(t, dir, "entities/post.vuka", `package entities

import "github.com/vuka-lang/orm/v2"

type Post struct {
	ID int64 @orm.PK
}
`)

	organizesTo(t, dir, "pages/home.vuka.html", `<script lang="go">
package pages

import (
	"strings"

	"example.com/shop/components"
	"example.com/shop/layouts"
)

type Home struct {
	Name string
}
</script>

<layouts.Applicant>
  <components.Logo/>
  <p>{strings.ToUpper(Name)} visits example.com</p>
</layouts.Applicant>
`)
}

// TestOrganizeVars: a context processor's variable read by name (user)
// is no package to import (os/user); while the variables aren't known, only
// an attribute's package is added.
func TestOrganizeVars(t *testing.T) {
	src := "package store\n\nfunc Hi() string { return strings.ToUpper(user.Name) }\n\ntype T struct {\n\tID int64 @orm.PK\n}\n"
	dir := orgModule(t, map[string]string{"store/hi.vuka": src})
	path := filepath.Join(dir, "store/hi.vuka")
	for _, c := range []struct {
		vars func() map[string]bool
		want string
	}{
		{func() map[string]bool { return map[string]bool{"user": true} }, "import (\n\t\"strings\"\n\n\t\"github.com/vuka-lang/orm/v2\"\n)"},
		{nil, "import \"github.com/vuka-lang/orm/v2\""},
		{func() map[string]bool { return nil }, "import \"github.com/vuka-lang/orm/v2\""},
	} {
		o, err := organizeImports(path, []byte(src), os.ReadFile, c.vars)
		if err != nil || o.edit == nil || strings.TrimSpace(o.edit.text) != c.want {
			t.Errorf("organized: %+v %v, want %q", o.edit, err, c.want)
		}
	}
}

// TestOrganizeSyntaxError: a file whose code doesn't parse is left as it
// is.
func TestOrganizeSyntaxError(t *testing.T) {
	dir := orgModule(t, map[string]string{"store/half.vuka": "package store\n\nimport \"fmt\"\n\nfunc Half( {\n"})
	path := filepath.Join(dir, "store/half.vuka")
	src, _ := os.ReadFile(path)
	if o, err := organizeImports(path, src, os.ReadFile, noVars); err == nil || o.edit != nil {
		t.Errorf("organized a file that doesn't parse: %v %+v", err, o.edit)
	}
}

func TestNameGuesses(t *testing.T) {
	for path, want := range map[string]string{"github.com/mattn/go-sqlite3": "sqlite3", "gopkg.in/yaml.v3": "yaml", "github.com/x/foo-go": "foo"} {
		if g := nameGuesses(path); !strings.Contains(strings.Join(g, " "), want) {
			t.Errorf("%s: %v", path, g)
		}
	}
}

// TestFmtImports: vuka fmt -imports organizes the imports of the files
// it formats; with -l it lists those it would change.
func TestFmtImports(t *testing.T) {
	dir := orgModule(t, map[string]string{
		"store/store.vuka": "package store\n\nimport \"fmt\"\n\nfunc Up(s string) string { return strings.ToUpper(s) }\n",
		"store/ok.vuka":    "package store\n\nimport \"strings\"\n\nfunc Low(s string) string { return strings.ToLower(s) }\n",
	})
	chdir(t, dir)
	var out, errs bytes.Buffer
	if err := vukaFmt([]string{"-l", "-imports", "."}, &out, &errs); err != nil {
		t.Fatal(err, errs.String())
	}
	if got := strings.Fields(out.String()); len(got) != 1 || got[0] != filepath.Join("store", "store.vuka") {
		t.Errorf("-l -imports listed %q", got)
	}
	out.Reset()
	if err := vukaFmt([]string{"-l", "."}, &out, &errs); err != nil || out.Len() != 0 {
		t.Errorf("-l without -imports listed %q (%v)", out.String(), err)
	}
	if err := vukaFmt([]string{"-w", "-imports", "store"}, &out, &errs); err != nil {
		t.Fatal(err, errs.String())
	}
	if got, want := readFile(t, filepath.Join(dir, "store/store.vuka")), "package store\n\nimport \"strings\"\n\nfunc Up(s string) string { return strings.ToUpper(s) }\n"; got != want {
		t.Errorf("-w -imports wrote:\n%s", got)
	}
}
