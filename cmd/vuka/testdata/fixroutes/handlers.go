package main

import (
	"context"

	"example.com/app/pages/urls"
)

func next(ctx context.Context, id int) string {
	orgs := urls.Orgs
	_ = orgs
	return urls.Orgs.Org(id).URL(ctx)
}
