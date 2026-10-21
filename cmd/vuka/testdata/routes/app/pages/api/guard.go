package api

import (
	"context"
	"errors"
)

// Guard refuses the API without a token.
type Guard struct{}

func (g *Guard) Load(ctx context.Context) error { return errors.New("no token") }
