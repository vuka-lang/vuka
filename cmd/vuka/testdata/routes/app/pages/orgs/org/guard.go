package org

import "context"

// Guard lets a member in.
type Guard struct{ Params }

func (g *Guard) Load(ctx context.Context) error { return nil }
