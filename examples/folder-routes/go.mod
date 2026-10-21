// The example's routes are its pages/ folders, which need the vuka command's
// folder routes and web's LayoutOf and Rest (1.1), its endpoints, guards
// and generated constructors and web's GuardOf (1.2), and its middleware
// and web's Use on folder routes (1.3); cmd/vuka's TestFolderRoutesE2E
// builds it against local checkouts.
module example.com/folder-routes

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
