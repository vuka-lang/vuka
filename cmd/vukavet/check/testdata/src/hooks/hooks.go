package hooks

import (
	"context"

	"github.com/vuka-lang/orm/v2"
)

type User struct {
	orm.Model[User]
	ID    int64
	Email string
}

func (User) Indexes() []orm.IndexKey { return nil }  // want `W0503: User.Indexes looks like an ORM hook, but its signature is func \(User\) Indexes\(\) \[\]orm.IndexKey; it must be func \(User\) Indexes\(\) \[\]orm.Index: the ORM ignores it otherwise`
func (*User) Checks() []orm.Index    { return nil }  // want `W0503: User.Checks looks like an ORM hook, but its signature is func \(\*User\) Checks\(\) \[\]orm.Index; it must be func \(\*User\) Checks\(\) \[\]orm.RowCheck`
func (u *User) BeforeSave()          {}              // want `W0503: User.BeforeSave looks like an ORM hook, but its signature is func \(\*User\) BeforeSave\(\); it must be func \(\*User\) BeforeSave\(context.Context\) error`
func (u *User) Validate() bool       { return true } // want `W0503: User.Validate looks like an ORM hook, but its signature is func \(\*User\) Validate\(\) bool; it must be func \(\*User\) Validate\(context.Context\) error`

// Post's hooks are the ORM's, with either receiver.
type Post struct {
	orm.Base[Post]
	ID    int64
	Title string
}

func (Post) Indexes() []orm.Index                      { return nil }
func (*Post) Checks() []orm.RowCheck                   { return nil }
func (Post) Generated() map[string]orm.Expr            { return nil }
func (p *Post) BeforeCreate(ctx context.Context) error { return nil }
func (p Post) AfterDelete(c context.Context) error     { return nil }

// Stamps is no model: its methods are its own.
type Stamps struct{ Created int64 }

func (Stamps) Indexes() []string { return nil }

// Order embeds a model through Audited.
type Audited struct {
	orm.Model[Order]
	Stamps
}

type Order struct {
	Audited
	ID int64
}

func (o *Order) AfterSave(ctx context.Context) {} // want `W0503: Order.AfterSave looks like an ORM hook`
