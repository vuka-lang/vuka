package pushsteps

import "github.com/vuka-lang/ui/v2"

type Orders struct{}

func (p *Orders) Save() error { return nil }

func (p *Orders) chains() []ui.Commands {
	return []ui.Commands{
		ui.JS.Push(p.Save).Hide("#form"),                      // want `W0116: Hide runs at once, while the Push before it is on its way, not after its reply`
		ui.JS.Push(p.Save).Timeout(3).Navigate("/orders"),     // want `W0116: Navigate runs at once`
		ui.JS.Push(p.Save).Into("#x").Patch("/orders?page=2"), // want `W0116: Patch runs at once`
		ui.JS.Push(p.Save).Then(ui.ShowToast("saved")),        // want `W0116: the toast runs at once`
		ui.JS.Push(p.Save).Reload(),                           // want `W0116: Reload runs at once`
		ui.JS.Push(p.Save).Success(ui.JS.Reload()),
		ui.JS.Push(p.Save).Success(ui.JS.Hide("#form")),
		ui.JS.Hide("#form").Push(p.Save),
		ui.JS.Push(p.Save).Show("#spinner"),
		ui.JS.Call("confetti").Hide("#x"),
	}
}
