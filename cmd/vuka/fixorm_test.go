package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
)

func TestOrmTagAttrs(t *testing.T) {
	for _, c := range []struct{ tag, attrs, rest string }{
		{`orm:"elements:placement_id"`, `@orm.Elements{Column: "placement_id"}`, ``},
		{`json:"tags" orm:"elements:tag_ids,code"`, `@orm.Elements{Column: "tag_ids", Ref: "code"}`, `json:"tags"`},
		{`orm:"column:headline;unique;index"`, `@orm.Column("headline") @orm.Unique @orm.Index`, ``},
		{`orm:"pk;auto_now_add"`, `@orm.PK @orm.AutoNowAdd`, ``},
		{`orm:"type:jsonb;vector:384"`, `@orm.DBType("jsonb") @orm.Dims(384)`, ``},
		{`orm:"on_delete:cascade;related:posts;crossdb"`, `@orm.Rel{OnDelete: orm.Cascade, Related: "posts", CrossDB: true}`, ``},
		{`orm:"-" json:"-"`, `@orm.Skip`, `json:"-"`},
		{`orm:"embedded;prefix:home_;column:x"`, `@orm.Column("x")`, `orm:"embedded;prefix:home_"`},
		{`json:"name"`, ``, `json:"name"`},
	} {
		attrs, rest := ormTagAttrs(c.tag, "orm")
		if got := strings.Join(attrs, " "); got != c.attrs || rest != c.rest {
			t.Errorf("ormTagAttrs(%s) = %s | %s, want %s | %s", c.tag, got, rest, c.attrs, c.rest)
		}
	}
}

func TestAttributeTagsModel(t *testing.T) {
	src := "package entities\n\nimport \"github.com/vuka-lang/orm/v2\"\n\n" +
		"type Employer struct {\n\torm.Base @orm.Meta{Table: \"employers\"}\n\tID int64\n" +
		"\tPlacements []Placement `json:\"placements\" orm:\"elements:placement_id\"`\n" +
		"\tName string `orm:\"column:employer_name\"` @orm.Char{Max: 100}\n}\n\n" +
		"type Plain struct {\n\tTags []string `orm:\"elements:tag_ids\"`\n}\n"
	out, n, err := attributeTags([]byte(src))
	if err != nil || n != 2 {
		t.Fatalf("%d fields changed, %v:\n%s", n, err, out)
	}
	for _, want := range []string{
		"Placements []Placement `json:\"placements\"` @orm.Elements{Column: \"placement_id\"}",
		"Name       string      @orm.Column(\"employer_name\") @orm.Char{Max: 100}",
		"Tags []string `orm:\"elements:tag_ids\"`", // not a model
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
}

func TestDBSchemaRefs(t *testing.T) {
	src := "package entities\n\nimport \"github.com/vuka-lang/orm/v2\"\n\n" +
		"var legacy = orm.DBSchema{Names: \"x\"}\n" +
		"var tenant orm.Schema\n\n" +
		"func (a *Applicant) Schema(s *orm.Schema) {}\n\n" +
		"func of() { orm.Of[Applicant](orm.Schema{}); _ = \"orm.Schema{}\" }\n"
	out, lines := dbSchemaRefs([]byte(src))
	want := strings.NewReplacer("var tenant orm.Schema", "var tenant orm.DBSchema", "orm.Of[Applicant](orm.Schema{})", "orm.Of[Applicant](orm.DBSchema{})").Replace(src)
	if string(out) != want || strings.Join(lines, ",") != "6,10" {
		t.Errorf("lines %v:\n%s\nwant:\n%s", lines, out, want)
	}
}

const queriesSrc = `package users

import (
	"context"
	"strings"

	"github.com/vuka-lang/orm/v2"
	"github.com/vuka-lang/orm/v2/fn"
)

type userRow struct {
	orm.Base
	ID       int64
	Username string
	Email    string
}

type groupMemberRow struct {
	orm.Base
	UserID  int64
	GroupID int64
}

type permissionRow struct {
	orm.Base
	ID      int64
	GroupID int64
}

func find(ctx context.Context, login string, id int64) Result[Option[userRow]] {
	found := userRow.Objects.Filter(vuka.Or(userRow.Username.Eq(login), fn.Lower(userRow.Email).Eq(strings.ToLower(login)))).First(ctx)?
	row, ok := found.Get()
	if !ok {
		return Ok(None[userRow]())
	}
	groups := groupMemberRow.Objects.Filter(groupMemberRow.UserID.Eq(id))
	perms := permissionRow.Objects.Filter(fn.In(permissionRow.GroupID, groups, groupMemberRow.GroupID)).All(ctx)?
	first := permissionRow.Objects.First(ctx)?
	upsert(ctx, userRow.Objects.Manager(), &row)
	_ = vuka.Or(other.A.Eq(1))
	return Ok(Some(row))
}
`

func TestQueryFixes(t *testing.T) {
	out, n := queryFixes([]byte(queriesSrc), map[string]bool{"userRow": true, "groupMemberRow": true, "permissionRow": true})
	if n != 5 {
		t.Errorf("%d rewritten, want 5", n)
	}
	for _, want := range []string{
		"found := orm.Optional(userRow.Objects.Filter(orm.Or(userRow.Username.Eq(login), orm.Lower(userRow.Email).Eq(strings.ToLower(login)))).First(ctx))?",
		"permissionRow.Objects.Filter(permissionRow.GroupID.InQuery(groups.Values[any](groupMemberRow.GroupID.Name()))).All(ctx)?",
		"first := permissionRow.Objects.First(ctx)?", // read as a row
		"upsert(ctx, userRow.Objects, &row)",
		"_ = vuka.Or(other.A.Eq(1))", // no model's
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
	if strings.Contains(string(out), "orm/fn") {
		t.Errorf("fn is still imported:\n%s", out)
	}
}

// TestEjectORM ejects models and queries written in the orm's earlier
// forms — orm: tags, queries made for orm.Query, orm.Schema values — and
// runs the Go: eject -rm writes them as they are written today.
func TestEjectORM(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	if bi, ok := debug.ReadBuildInfo(); !ok || !slices.ContainsFunc(bi.Deps, func(m *debug.Module) bool {
		return m.Path == "github.com/vuka-lang/vuka/v2" && m.Replace != nil
	}) {
		t.Skip("needs this checkout's transpiler: go mod edit -replace github.com/vuka-lang/vuka/v2=../..")
	}
	repo, _ := filepath.Abs("../..")
	web, orm := os.Getenv("VUKA_WEB"), os.Getenv("VUKA_ORM")
	if web == "" {
		web = filepath.Join(filepath.Dir(repo), "vuka-web")
	}
	if orm == "" {
		orm = filepath.Join(filepath.Dir(repo), "vuka-orm")
	}
	if src, _ := os.ReadFile(filepath.Join(orm, "queryset.go")); !bytes.Contains(src, []byte("func Optional[")) {
		t.Skipf("github.com/vuka-lang/orm/v2 at %s has no Optional; set VUKA_ORM", orm)
	}
	root := localModule(t, web, map[string]string{"main.vuka": ejectORMSrc})
	withModule(t, root, "github.com/vuka-lang/orm/v2", orm)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(root)
	if err := eject([]string{"-rm", "."}, io.Discard); err != nil {
		t.Fatal(err)
	}
	src, _ := os.ReadFile("main.go")
	for _, want := range []string{
		`s.Field(&n.Tags, orm.Elements{Column: "tag_list"})`,
		`s.Field(&n.Title, orm.Column("headline"))`,
		`orm.Optional(orm.Objects[Note]().Filter(orm.Or(NoteFields.Title.Eq(title), orm.Lower(NoteFields.Title).Eq(strings.ToLower(title)))).First(ctx))`,
		`orm.Of[Note](orm.DBSchema{})`,
	} {
		if !bytes.Contains(src, []byte(want)) {
			t.Errorf("no %s in the ejected main.go:\n%s", want, src)
		}
	}
	out, err := exec.Command("go", "run", ".").CombinedOutput()
	if err != nil || string(out) != "found Go\nnone\n" {
		t.Errorf("go run after vuka eject -rm: %v\n%s", err, out)
	}
}

const ejectORMSrc = `package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/vuka-lang/orm/v2"
	"github.com/vuka-lang/orm/v2/fn"
	_ "github.com/vuka-lang/orm/v2/sqlite"
)

type Tag struct {
	orm.Base
	ID   int64
	Name string
}

type Note struct {
	orm.Base
	ID    int64
	Title string ` + "`json:\"title\" orm:\"column:headline\"`" + `
	TagList string
	Tags    []Tag  ` + "`orm:\"elements:tag_list\"`" + `
}

func find(ctx context.Context, title string) Result[Option[Note]] {
	found := Note.Objects.Filter(vuka.Or(Note.Title.Eq(title), fn.Lower(Note.Title).Eq(strings.ToLower(title)))).First(ctx)?
	n, ok := found.Get()
	if !ok {
		return Ok(None[Note]())
	}
	return Ok(Some(n))
}

func main() {
	ctx := context.Background()
	db, err := orm.ConnectURL("sqlite::memory:")
	if err != nil {
		panic(err)
	}
	orm.AddDatabase("main", db, orm.AsDefault())
	_ = orm.Of[Note](orm.Schema{})
	if err := orm.CreateTables(ctx, Tag.Objects.Manager(), Note.Objects.Manager()); err != nil {
		panic(err)
	}
	if err := Note.Objects.Create(ctx, &Note{Title: "Go"}); err != nil {
		panic(err)
	}
	for _, q := range []string{"go", "rust"} {
		if n, ok := find(ctx, q).Unwrap().Get(); ok {
			fmt.Println("found", n.Title)
		} else {
			fmt.Println("none")
		}
	}
}
`

// TestEjectFixesHooks: eject -rm marks the hooks a live page's methods
// were found by, so the Go it writes declares them in LiveSchema.
func TestEjectFixesHooks(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.24\n"), 0o644)
	os.WriteFile(filepath.Join(root, "orders.vuka"), []byte(tagPage), 0o644)
	t.Chdir(root)
	if err := ejectFixes([]string{"."}, nil, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	src, _ := os.ReadFile("orders.vuka")
	for _, want := range []string{"@ui.Defer(\"lists\")", "@ui.Loads(\"lists\")\nfunc (o *Orders) LoadLists(", "@ui.Mount\nfunc (o *Orders) Mount("} {
		if !strings.Contains(string(src), want) {
			t.Errorf("missing %q in\n%s", want, src)
		}
	}
}
