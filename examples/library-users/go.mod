// The example builds against the vuka, ui, web and orm that have the
// registration API (web.Routes, App.Service, ui.Island as a route's
// option), ui.LiveSchema and orm's Schema; cmd/vuka's TestLibraryExample
// builds it against local checkouts.
module example.com/library-users

go 1.25.0

require (
	github.com/vuka-lang/orm v0.9.1
	github.com/vuka-lang/ui v0.50.0
	github.com/vuka-lang/vuka v0.50.0
	github.com/vuka-lang/web v0.50.0
)
