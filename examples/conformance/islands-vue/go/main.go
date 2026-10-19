// Command islands-vue is a live page in plain Go placing a Vue island: its
// props following the page, its emit the page's event. It builds with Node.
package main

import (
	"log"

	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

//go:generate go tool vuka gen -inplace

// Picker is an island, islands/Picker.vue.
type Picker struct {
	Label   string   `json:"label"`
	Options []string `json:"options"`
	Picked  string   `json:"picked"`
}

// Settings places the picker.
type Settings struct {
	ui.Live
	theme string
}

// Choose takes the picker's pick.
func (s *Settings) Choose(v string) { s.theme = v }

func (s *Settings) Render() ui.Node {
	picker := ui.Mount(Picker{Label: "Theme", Options: []string{"light", "dark"}, Picked: s.theme}, ui.Emit("pick", s.Choose))
	return ui.HTML(`
<main>
  {picker}
  <p class="theme">{s.theme}</p>
</main>`, picker, s.theme)
}

func init() {
	ui.RegisterIsland[Picker]()
	web.Routes.Live("/", func() *Settings { return &Settings{} })
}

func main() {
	app := web.New()
	log.Fatal(app.Run(":" + web.Env("PORT", "8080")))
}
