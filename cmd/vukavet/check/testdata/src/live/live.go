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

var query string

type Search struct {
	ui.Live
	Q       string
	Rows    []string
	Stats   int
	Pending bool
	Name    string
	hidden  string
	Both    int
}

func NewSearch() *Search          { return &Search{} }
func (s *Search) Render() ui.Node { return nil }

func (s *Search) LiveSchema(l *ui.LiveSchema) {
	l.Field(&s.Q, ui.Bind{}, ui.URL("q"))
	l.Field(&s.Rows, ui.Defer("rows"))
	l.Field(&s.Stats, ui.Defer("stats"))         // want `E0324: Search.Stats is deferred, and the group Stats has no loader`
	l.Field(&s.Pending, ui.Bind{})               // want `E0322: Search.Pending: live.pending is live's own`
	l.Field(&s.hidden, ui.Bind{})                // want `E0322: Search.hidden: ui.Bind is for a prop`
	l.Field(&s.Both, ui.Defer("x"), ui.URL("x")) // want `E0323: Search.Both: a deferred field is the server's to load`
	l.Field(&query, ui.Bind{})                   // want `E0335: Search.LiveSchema: l.Field is given &query, not the address of one of Search's fields`
	l.Field(&s.Name, nil)                        // want `E0335: Search.LiveSchema: l.Field\(&s.Name, …\) is given a nil attribute`
	l.Loads("rows", s.loadRows)
	l.Loads("rows", s.loadRows) // want `E0336: Search.LiveSchema: l.Loads\("rows", …\) is called twice`
	l.Loads("a-b", s.loadRows)  // want `E0336: Search.LiveSchema: l.Loads\("a-b", …\): a group is a name`
	l.Mount(nil)                // want `E0336: Search.LiveSchema: l.Mount is given nil`
	l.OnChange(s.Changed)
}

func (s *Search) loadRows(ctx context.Context) error { return nil }

// Changed is the OnChange hook, so any signature its method value fits.
func (s *Search) Changed(ctx context.Context, c ui.Changed) error { return nil }

func init() {
	web.Routes.Live("/search", NewSearch, ui.Island())
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
