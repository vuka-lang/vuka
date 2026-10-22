// Package pages is the app's pages: Dashboard at /, and the profiles at
// /users/{id}.
package pages

import "github.com/vuka-lang/ui/v2"

// Chart is an island, web/islands/Chart.vue: its exported fields are the
// component's props.
type Chart struct {
	Title  string `json:"title"`
	Points []int  `json:"points"`
}

func init() { ui.RegisterIsland[Chart]() }
