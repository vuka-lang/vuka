package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/vuka/v2"
	ui "vukatest.local/stateui"
)

type Employee struct {
	Name     vuka.Option[string]
	Status   vuka.Option[string]
	Verified vuka.Option[bool]
	Age      vuka.Option[int]
	Tags     vuka.Option[[]string]
}

func main() {
	full := Employee{Name: vuka.Some("Ann"), Status: vuka.Some("active"), Verified: vuka.Some(true), Age: vuka.Some(40), Tags: vuka.Some([]string{"a", "b"})}
	for _, e := range []Employee{full, {}} {
		out, err := ui.String(context.Background(), Page{E: e})
		fmt.Println(out, err)
	}
}
