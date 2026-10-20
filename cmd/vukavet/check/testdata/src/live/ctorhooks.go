package live

import (
	"context"

	"github.com/vuka-lang/ui"
	"github.com/vuka-lang/web"
)

type Orders struct {
	ui.Live
	Q    string   `json:"q"`
	Rows []string `json:"rows" defer:"rows"`
}

func NewOrders() *Orders {
	o := &Orders{}
	o.OnMount((*Orders).mount)
	o.Loads("rows", (*Orders).loadRows)
	o.OnChange((*Orders).changed)
	return o
}

func (o *Orders) mount(ctx context.Context) error    { return nil }
func (o *Orders) loadRows(ctx context.Context) error { return nil }
func (o *Orders) changed(c ui.Changed)               {}
func (o *Orders) Render() ui.Node                    { return nil }

type BadHooks struct {
	ui.Live
}

func NewBadHooks() *BadHooks {
	b := &BadHooks{}
	b.OnMount(b.mount)                     // want `E0336: BadHooks.OnMount is given b.mount, not a method expression of the page`
	b.OnChange((*BadHooks).wrong)          // want `E0336: BadHooks.OnChange is given \(\*BadHooks\).wrong, a func\(n int\)`
	b.Loads("two words", (*BadHooks).load) // want `E0336: BadHooks.Loads\("two words", …\): a group is a name`
	b.Loads("rows", (*Orders).loadRows)    // want `E0336: BadHooks.Loads\(rows\) is given a method of another type`
	b.OnMount((*BadHooks).Start)           // want `E0336: BadHooks.OnMount is given \(\*BadHooks\).Start, an exported method`
	return b
}

func (b *BadHooks) Start() {}

func (b *BadHooks) mount()                         {}
func (b *BadHooks) wrong(n int)                    {}
func (b *BadHooks) load(ctx context.Context) error { return nil }
func (b *BadHooks) Render() ui.Node                { return nil }

type Twice struct {
	ui.Live
}

func NewTwice() *Twice {
	t := &Twice{}
	t.OnMount((*Twice).mount)
	t.OnMount((*Twice).start) // want `E0336: Twice.OnMount is declared twice, as mount and as start`
	return t
}

func (t *Twice) mount()          {}
func (t *Twice) start()          {}
func (t *Twice) Render() ui.Node { return nil }

type Both struct {
	ui.Live
}

func NewBoth() *Both {
	b := &Both{}
	b.OnMount((*Both).mount) // want `E0336: Both.OnMount is declared by its constructor and by its LiveSchema`
	return b
}

func (b *Both) LiveSchema(l *ui.LiveSchema) { l.Mount(b.mount) }
func (b *Both) mount(context.Context) error { return nil }
func (b *Both) Render() ui.Node             { return nil }

type Counter struct {
	ui.Live
	N int
}

func (c *Counter) mount()          {}
func (c *Counter) Render() ui.Node { return nil }

func setup(c *Counter) {
	c.OnMount((*Counter).mount) // want `W0305: Counter.OnMount is declared in setup, which doesn't return the page`
}

var counter = &Counter{N: 1} // want `W0304: Counter is built as a literal, and its hooks are declared by its constructor`

func init() {
	web.Routes.Live("/orders", NewOrders, ui.Island())
	web.Routes.Live("/bad-hooks", NewBadHooks)
	web.Routes.Live("/twice", NewTwice)
	web.Routes.Live("/both", NewBoth)
}
