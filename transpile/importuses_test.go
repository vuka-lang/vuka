package transpile

import (
	"reflect"
	"sort"
	"testing"
)

func usedNames(t *testing.T, file File, others ...File) []string {
	t.Helper()
	u, err := Uses(file, Options{}, others...)
	if err != nil {
		t.Fatalf("%s: %v", file.Name, err)
	}
	var out []string
	for n := range u.Qualifiers {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func TestUses(t *testing.T) {
	tmpl := File{Name: "home.vuka.html", Src: []byte(`<script lang="go">
package pages

import (
	"strings"

	"example.com/app/components"
)

type Home struct {
	Name string
}
</script>

<main>
  <components.Logo size={fmt.Sprint(2)}/>
  <p>{strings.ToUpper(Name)} visit example.com</p>
  <a href={urls.Root.Path()}>home</a>
  <layouts.Applicant>x</layouts.Applicant>
</main>
`)}
	if got, want := usedNames(t, tmpl), []string{"components", "fmt", "layouts", "strings", "urls"}; !reflect.DeepEqual(got, want) {
		t.Errorf("template: %v, want %v", got, want)
	}
	model := File{Name: "user.vuka", Src: []byte(`package entities

import "fmt"

type User struct {
	ID int64 @orm.PK
	Name string
}

@graphql.Query
func Users(ctx context.Context) ([]User, error) {
	strings := []string{"a"}
	_ = strings.Len
	var u User
	_ = u.Name
	return nil, nil
}
`)}
	if got, want := usedNames(t, model), []string{"context", "graphql", "orm"}; !reflect.DeepEqual(got, want) {
		t.Errorf("model: %v, want %v", got, want)
	}
}
