module github.com/vuka-lang/vuka/cmd/vuka

go 1.25.0

require (
	github.com/a-h/parse v0.0.0-20250122154542-74294addb73e
	github.com/a-h/templ v0.3.1020
	github.com/evanw/esbuild v0.28.2
	github.com/vuka-lang/vuka v0.19.0
)

require golang.org/x/sys v0.41.0

replace github.com/vuka-lang/vuka => ../..
