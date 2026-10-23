package users

import (
	"context"

	"github.com/vuka-lang/web/v2/auth"
)

type User struct{ Name string }

type Vars struct{ Me *User }

func Context(ctx context.Context) (Vars, error) { return Vars{Me: lookup(ctx)}, nil }

func lookup(ctx context.Context) *User {
	if auth.Current(ctx) == nil {
		return nil
	}
	return &User{}
}

// Other gives Me too.
func Other(ctx context.Context) (Vars, error) { return Vars{}, nil }

// Count isn't a context processor.
func Count(ctx context.Context) int { return 0 }
