package main

import (
	"context"
	"fmt"
	"net/url"

	"github.com/vuka-lang/ui/v2"
)

func main() {
	u, _ := url.Parse("/boards/7?requests.page=3&requests.sort=date&users.sort=size")
	ctx := ui.WithPageURL(context.Background(), u, func(string) string { return "7" })
	out, err := ui.String(ctx, Board{}.View())
	fmt.Println(out, err)
}
