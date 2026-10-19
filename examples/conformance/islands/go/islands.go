package main

import (
	"fmt"

	"github.com/vuka-lang/ui"
)

// Chart is an island, islands/Chart.ts: its exported fields are its props.
type Chart struct {
	Title  string `json:"title"`
	Points []int  `json:"points"`
}

// Badge is an island, islands/Badge.ts, loaded when the browser is idle.
type Badge struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

// Range is what the chart emits when a span of it is selected.
type Range struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// Dashboard places the chart; Add grows it, the chart's select zooms it.
type Dashboard struct {
	ui.Live
	points []int
	zoom   string
}

func NewDashboard() *Dashboard { return &Dashboard{points: []int{3, 1, 4}} }

// Add adds a point.
func (d *Dashboard) Add() { d.points = append(d.points, len(d.points)) }

// Zoom takes the range the chart selected.
func (d *Dashboard) Zoom(r Range) { d.zoom = fmt.Sprintf("%d–%d", r.From, r.To) }

func (d *Dashboard) Render() ui.Node {
	chart := ui.Mount(Chart{Title: "Sales", Points: d.points}, ui.Visible(), ui.Emit("select", d.Zoom))
	return ui.HTML(`
<main>
  <button id="add" onClick={d.Add}>Add</button>
  {chart}
  {zoom}
</main>`, d.Add, chart, ui.If(d.zoom != "", ui.HTML(`<p class="zoom">{d.zoom}</p>`, d.zoom)))
}

// About is a page that isn't live, with an island in it.
func About() ui.Node {
	return ui.HTML(`
<html>
  <head><title>About</title></head>
  <body>
    <h1>About</h1>
    {ui.Mount(Badge{Label: "islands", Count: 2}, ui.Idle())}
  </body>
</html>`, ui.Mount(Badge{Label: "islands", Count: 2}, ui.Idle()))
}
