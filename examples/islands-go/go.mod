// The example builds against the vuka, ui and web that build islands for
// plain Go (ui.RegisterIsland bare, islands in the frontend's build);
// cmd/vuka's TestIslandsGo builds it against local checkouts.
module example.com/islands-go

go 1.25.0

require (
	github.com/vuka-lang/ui v0.50.0
	github.com/vuka-lang/vuka v0.50.0
	github.com/vuka-lang/web v0.50.0
)
