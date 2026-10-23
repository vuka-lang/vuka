package settings

import "context"

type Vars struct{ SiteName string }

func Context(ctx context.Context) (Vars, error) { return Vars{SiteName: "shop"}, nil }
