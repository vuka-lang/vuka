// Package auth stands in for github.com/vuka-lang/web/v2/auth's context
// processor.
package auth

import (
	"context"
	"path"
)

// Identity is a signed-in user.
type Identity struct{ ID, Name string }

// Perms are a user's permissions.
type Perms []string

// Has reports whether p grants perm, with wildcards.
func (p Perms) Has(perm string) bool {
	for _, g := range p {
		if ok, _ := path.Match(g, perm); ok || g == "*" {
			return true
		}
	}
	return false
}

// Vars are auth.Context's variables.
type Vars struct {
	User  *Identity
	Perms Perms
}

// Context stands in for the built-in processor.
func Context(ctx context.Context) (Vars, error) { return Vars{}, nil }

// Other gives a user too: listed beside Context, the names clash.
func Other(ctx context.Context) (Vars, error) { return Vars{}, nil }

// Can stands in for auth.Can: whether the user has perm.
func Can(ctx context.Context, perm string) bool { return false }
