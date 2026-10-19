package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui"
)

// Chart is an island registered in Go, islands/Chart.ts.
type Chart struct {
	Title  string `json:"title"`
	Points []int  `json:"points"`
}

func init() { ui.RegisterIsland[Chart]() }

type Page struct{ Points []int }

func main() {
	out, err := ui.String(context.Background(), Page{Points: []int{3, 1}})
	fmt.Println(out, err)
}
