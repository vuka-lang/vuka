package main

import (
	"fmt"

	"github.com/vuka-lang/ui"
)

// Item is a picture of the gallery.
type Item struct {
	ID    int
	Name  string
	Tags  []string
	Liked bool
}

// Gallery lists the items in cards, each with its own counter
// (gallery.vuka.html).
type Gallery struct {
	ui.Live
	items []Item
	wide  bool
}

func NewGallery() *Gallery {
	return &Gallery{items: []Item{{1, "Sunrise", []string{"sky", "red"}, false}, {2, "Harbour", []string{"sea"}, true}, {3, "Forest", nil, false}}}
}

// Widen toggles the cards' width.
func (g *Gallery) Widen() { g.wide = !g.wide }

// Like toggles an item's like.
func (g *Gallery) Like(id int) {
	for i := range g.items {
		if g.items[i].ID == id {
			g.items[i].Liked = !g.items[i].Liked
		}
	}
}

// Drop removes the first item, keeping the others' counters.
func (g *Gallery) Drop() {
	if len(g.items) > 0 {
		g.items = g.items[1:]
	}
}

func accent(it Item) string { return fmt.Sprintf("#%d%d%d", it.ID, it.ID, it.ID) }

func likeText(liked bool) string {
	if liked {
		return "Unlike"
	}
	return "Like"
}
