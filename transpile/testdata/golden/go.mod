// The golden cases are a module of their own, as a user's would be: it
// requires the JSX target the JSX cases import, github.com/vuka-lang/ui.
module golden

go 1.25.0

require (
	github.com/a-h/templ v0.3.1020
	github.com/vuka-lang/ui v0.50.1-0.20261019124552-8480fcfa16ef
	github.com/vuka-lang/vuka v0.50.0
	vukatest.local/islandui v0.0.0
	vukatest.local/stateui v0.0.0
	vukatest.local/webstub v0.0.0
)

replace github.com/vuka-lang/vuka => ../../..

replace vukatest.local/islandui => ./islandui

replace vukatest.local/stateui => ./stateui

replace vukatest.local/webstub => ./webstub
