package auth

import "context"

type Identity struct{ ID, Name string }

type Perms []string

func (p Perms) Has(perm string) bool { return false }

func Current(ctx context.Context) *Identity { return nil }

type Vars struct {
	User  *Identity
	Perms Perms
}

func Context(ctx context.Context) (Vars, error) { return Vars{User: Current(ctx)}, nil }
