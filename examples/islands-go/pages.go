package main

import (
	"context"

	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

// Chart is an island, web/islands/Chart.vue: its exported fields are the
// component's props.
type Chart struct {
	Title  string `json:"title"`
	Points []int  `json:"points"`
}

func init() { ui.RegisterIsland[Chart]() }

// Dashboard is a live page placing a Chart.
type Dashboard struct {
	ui.Live
	points []int
}

func NewDashboard() *Dashboard { return &Dashboard{points: []int{3, 1, 4}} }

// Add is the button's event: the chart gets the new point.
func (d *Dashboard) Add() { d.points = append(d.points, len(d.points)) }

func (d *Dashboard) Render() ui.Node {
	return ui.El("main", nil,
		ui.El("button", []ui.Attr{{Name: "id", Value: "add"}, {Name: "onClick", Value: ui.On(d.Add)}}, ui.Text("Add")),
		ui.Mount(Chart{Title: "Sales", Points: d.points}, ui.Load()))
}

// Profile is a live page backed by its island, web/islands/Profile.vue.
type Profile struct {
	ui.Live
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func NewProfile() *Profile { return &Profile{} }

func (p *Profile) LiveSchema(l *ui.LiveSchema) { l.Mount(p.mount) }

func (p *Profile) mount(ctx context.Context) error {
	names := map[int]string{1: "Ada", 2: "Grace"}
	if p.Name = names[p.ID]; p.Name == "" {
		return web.NotFound
	}
	return nil
}

func (p *Profile) Render() ui.Node { return ui.IslandPage(p) }

// page is the profile's layout.
func page(content ui.Node) ui.Node {
	return ui.El("html", nil, ui.El("head", nil, ui.El("title", nil, ui.Text("Islands"))), ui.El("body", nil, content))
}
