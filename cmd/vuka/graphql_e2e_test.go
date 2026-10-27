package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const graphqlMain = `package main

import (
	"example.com/app/pets"
	"github.com/vuka-lang/web/v2"
	"github.com/vuka-lang/web/v2/graphql"
)

func main() {
	app := web.New(web.GraphQL("/graphql", graphql.Naming{Fields: graphql.SnakeCase}))
	app.Service(pets.NewStore)
	app.Run("")
}
`

const graphqlTypes = `package pets

// Pet is an animal for sale.
type Pet struct {
	ID        int64 ` + "`graphql:\",id\"`" + `
	Name      string
	BirthYear int
}

type Store struct{ pets []Pet }

func NewStore() *Store { return &Store{} }
`

const graphqlOps = `package pets

import (
	"context"

	"github.com/vuka-lang/web/v2/graphql"
)

// Pets lists the pets.
@graphql.Query
func Pets(ctx context.Context, first int, store *Store) ([]Pet, error) { return store.pets, nil }
`

// TestGraphQLSchema: vuka graphql schema prints the schema the app builds —
// names decided by its options — writes it, and checks a changed one,
// failing on a breaking change only.
func TestGraphQLSchema(t *testing.T) {
	repo, _ := filepath.Abs("../..")
	web := os.Getenv("VUKA_WEB")
	if web == "" {
		web = filepath.Join(filepath.Dir(repo), "vuka-web")
	}
	if !hasWebFunc(web, "GraphQL") {
		needs(t, "github.com/vuka-lang/web/v2 at %s has no GraphQL; set VUKA_WEB (and VUKA_UI)", web)
	}
	root := localModule(t, web, map[string]string{
		"main.go":       graphqlMain,
		"pets/types.go": graphqlTypes,
		"pets/ops.vuka": graphqlOps,
	})
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	for _, k := range []string{graphqlSchemaEnv, graphqlCheckEnv, "VUKA_TAILWIND", "VUKA_FRONTEND"} {
		t.Setenv(k, "")
	}
	t.Chdir(root)
	schema := func(args ...string) (string, error) {
		t.Helper()
		os.Unsetenv(graphqlSchemaEnv)
		os.Unsetenv(graphqlCheckEnv)
		var stdout, stderr bytes.Buffer
		err := graphqlCmd(append([]string{"schema"}, args...), &stdout, &stderr)
		if err != nil && !exitedWith(err) {
			t.Fatalf("vuka graphql schema %s: %v\n%s", strings.Join(args, " "), err, stderr.Bytes())
		}
		return stdout.String(), err
	}
	sdl, err := schema()
	want := `type Query {
  """Pets lists the pets."""
  pets(first: Int): [Pet!]!
}

"""Pet is an animal for sale."""
type Pet {
  id: ID!
  name: String!
  birth_year: Int!
}
`
	if err != nil || sdl != want {
		t.Fatalf("schema: %v\n%s\nwant:\n%s", err, sdl, want)
	}
	if _, err := schema("-o", "schema.graphql"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile("schema.graphql"); string(b) != want {
		t.Fatalf("schema.graphql:\n%s", b)
	}

	// Adding a field breaks nothing; making one nullable breaks clients.
	writeFile(t, "pets/types.go", strings.Replace(graphqlTypes, "BirthYear int", "BirthYear int\n\tNick *string", 1))
	if out, err := schema("-check", "schema.graphql"); err != nil || !strings.Contains(out, "Field Pet.nick was added") {
		t.Errorf("an added field: %v\n%s", err, out)
	}
	writeFile(t, "pets/types.go", strings.Replace(graphqlTypes, "Name      string", "Name      *string", 1))
	out, err := schema("-check", "schema.graphql")
	if !exitedWith(err) || !strings.Contains(out, "BREAKING Field Pet.name changed type from String! to String.") {
		t.Errorf("a breaking change: %v\n%s", err, out)
	}
}

// isExit reports whether err is the app's exit with a status.
func exitedWith(err error) bool {
	var exit *exec.ExitError
	var st exitStatus
	return errors.As(err, &exit) || errors.As(err, &st)
}
