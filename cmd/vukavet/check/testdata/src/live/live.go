package live

import (
	"context"

	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

type Board struct {
	ui.Live
	n int
}

func NewBoard() *Board             { return &Board{} }
func (b *Board) Render() ui.Node   { return nil }
func (b *Board) Mount(n int) error { return nil } // want `E0329: Board's Mount is func\(n int\) error`

type NoRender struct{ ui.Live } // want `E0329: NoRender is not a stateful component`

func NewNoRender() *NoRender { return &NoRender{} } // a struct without Render

type PtrLive struct {
	*ui.Live // want `E0329: PtrLive embeds \*ui.Live`
}

func (p *PtrLive) Render() ui.Node { return nil }

type Plain struct{ Name string } // want `E0330: Plain is a page backed by its island under Live`

type Address struct {
	City string `validate:"requird"` // want `E0328: City: validate:"requird": unknown rule "requird"`
}

type Detail struct {
	ui.Live
	Name    string `json:"name" bind:"nam"` // want `E0322: Detail.Name: bind:"nam" names the field live.name`
	Q       string `bind:",debounce=soon"`  // want `E0322: Detail.Q: bind:",debounce=soon": bad debounce "soon"`
	Pending bool   `bind:""`                // want `E0322: Detail.Pending: live.pending is live's own`
	hidden  string `bind:""`                // want `E0322: Detail.hidden: bind: is for a prop`
	CV      string `defer:"profile"`        // want `E0324: Detail.CV is deferred, and the group Profile has no loader`
	Stats   int    `defer:"stats"`
	Bad     int    `defer:"a-b"`       // want `E0323: Detail.Bad: defer:"a-b": a group is a name`
	Both    int    `defer:"x" url:"x"` // want `E0323: Detail.Both: a deferred field is the server's to load`
	Title   string `json:"name"`       // want `E0326: Detail: fields Name and Title are both live.name`
	Age     int    `validate:"min=x"`  // want `E0328: Detail.Age: validate:"min=x": min takes a number`
	Home    Address
}

func NewDetail() *Detail                           { return &Detail{} }
func (d *Detail) Render() ui.Node                  { return nil }
func (d *Detail) LoadStats(n int) error            { return nil } // want `E0325: Detail.LoadStats is the loader of the deferred group Stats, and it is func\(n int\) error`
func (d *Detail) Changed(x string)                 {}             // want `E0327: Detail.Changed is func\(x string\)`
func (d *Detail) Submit(ctx context.Context) error { return nil }

type Card struct {
	Title string `json:"title"`
}

func NewCard() *Card           { return &Card{} }
func (c *Card) Mount(a, b int) {} // want `E0333: Card's Mount is func\(a int, b int\)`

type Clash struct {
	ui.Live
	IsOpen bool `json:"open"`
}

func NewClash() *Clash           { return &Clash{} }
func (c *Clash) Render() ui.Node { return nil }
func (c *Clash) Open()           {} // want `E0326: Clash: the method Open and the field IsOpen are both live.open`

func init() {
	web.Routes.Live("/board", NewBoard)
	web.Routes.Live("/norender", NewNoRender)
	web.Routes.Live("/ptr", NewPtrLive)
	web.Routes.Live("/plain", NewPlain, ui.Island())
	web.Routes.Live("/detail", NewDetail, ui.Island())
	web.Routes.Get("/card", NewCard, ui.Island())
	web.Routes.Live("/x", 42)                              // want `E0329: Live\("/x", …\) takes the page's constructor, not a int`
	web.Routes.Live("/y", func() Board { return Board{} }) // want `E0329: Live\("/y", …\): a constructor returns a pointer to a struct`
	web.Routes.Live("/c", NewClash, ui.Island())
}

func NewPtrLive() *PtrLive { return &PtrLive{} }
func NewPlain() *Plain     { return &Plain{} }
