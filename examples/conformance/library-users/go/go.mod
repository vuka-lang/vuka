// The example builds against the vuka, ui, web and orm that have the
// registration API (web.Routes, App.Service, ui.Island as a route's
// option), ui.LiveSchema and orm's Schema; cmd/vuka's TestLibraryExample
// and TestConformance build it against local checkouts.
module example.com/library-users

go 1.27

require (
	github.com/vuka-lang/orm v1.0.0
	github.com/vuka-lang/ui v1.0.0
	github.com/vuka-lang/vuka v1.0.0
	github.com/vuka-lang/web v1.0.0
)

require (
	github.com/a-h/templ v0.3.1020 // indirect
	github.com/coder/websocket v1.8.15 // indirect
)
