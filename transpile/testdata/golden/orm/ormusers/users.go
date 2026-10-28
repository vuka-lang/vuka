// Package ormusers is a model package of another package's, as Go sees it:
// a model and the typed fields orm generate writes for it.
package ormusers

import "vukatest.local/orm"

// User is a model.
type User struct {
	orm.Model[User]
	ID   int64
	Name string
}

// UserFields is the generated receiver type.
type UserFields struct {
	ID   orm.Field[int64]
	Name orm.Field[string]
}

// UserColumns is the generated root lookup value.
var UserColumns = UserFields{orm.Field[int64]{"id"}, orm.Field[string]{"name"}}
