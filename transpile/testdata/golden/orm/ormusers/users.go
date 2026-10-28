// Package ormusers is a model package of another package's, as Go sees it:
// a model and the typed fields orm generate writes for it.
package ormusers

import "vukatest.local/orm"

// User is a model.
type User struct {
	orm.Model[User]
	ID       int64
	Name     string
	Category *Category
}

// UserFields is the generated receiver type.
type UserFields struct {
	ID   orm.Field[int64]
	Name orm.Field[string]
}

// UserColumns is the generated root lookup value.
var UserColumns = UserFields{orm.Field[int64]{"id"}, orm.Field[string]{"name"}}

// Category provides a relation hop in the new generated API.
type Category struct {
	orm.Model[Category]
	ID   int64
	Name string
}

type CategoryFields struct{ Name orm.Field[string] }

func (f UserFields) Category() CategoryFields {
	return CategoryFields{orm.Field[string]{"category__name"}}
}
