// The example builds against the vuka, ui and web that build islands for
// plain Go (ui.RegisterIsland bare, islands in the frontend's build);
// cmd/vuka's TestIslandsGo builds it against local checkouts.
module example.com/islands-go

go 1.25.0

require (
	github.com/vuka-lang/ui v1.0.0
	github.com/vuka-lang/vuka v1.0.0
	github.com/vuka-lang/web v1.0.0
)

require (
	github.com/a-h/templ v0.3.1020 // indirect
	github.com/coder/websocket v1.8.15 // indirect
)
