// The golden cases are a module of their own, as a user's would be: it
// requires the JSX target the JSX cases import. github.com/vuka-lang/ui is
// a sibling checkout until it is released.
module golden

go 1.25.0

require (
	github.com/a-h/templ v0.3.1020
	github.com/vuka-lang/ui v0.0.0
	github.com/vuka-lang/vuka v0.10.0
)

replace github.com/vuka-lang/vuka => ../../..

replace github.com/vuka-lang/ui => ../../../../vuka-ui
