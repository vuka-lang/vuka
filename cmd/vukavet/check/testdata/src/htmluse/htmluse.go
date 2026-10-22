package htmluse

import "github.com/vuka-lang/ui"

type Card struct{ Title string }

func (c Card) View() ui.Node {
	return ui.HTML(`<h2>{c.Title}</h2>`, c.Title) // want `W0106: ui.HTML is what the compiler writes for a .vuka.html file's markup; it is not an API to write by hand`
}

type Orders struct{}

func (o *Orders) Render() ui.Node {
	return ui.IslandPage(o) // want `W0106: ui.IslandPage is what the compiler writes for an island page, a .vuka.html file whose markup is its component's tag; it is not an API to write by hand`
}

func (c Card) Mislabelled() ui.Node {
	return ui.HTML(`<h2>{c.Title}</h2>`, c.Title+"!") // want `W0106: ui.HTML` `E0132: value 1 is c.Title \+ "!"; its label is \{c.Title\}`
}
