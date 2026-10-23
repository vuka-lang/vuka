// The golden cases are a module of their own, as a user's would be: it
// requires the JSX target the JSX cases import, github.com/vuka-lang/ui/v2.
module golden

go 1.25.0

require (
	github.com/a-h/templ v0.3.1020
	github.com/vuka-lang/ui/v2 v2.3.1-0.20261023182800-ff7cd1d0e53d
	github.com/vuka-lang/vuka/v2 v2.3.0
	github.com/vuka-lang/web/v2 v2.0.0
	vukatest.local/islandui v0.0.0
	vukatest.local/orm v0.0.0
	vukatest.local/stateui v0.0.0
	vukatest.local/ui v0.0.0
	vukatest.local/webstub v0.0.0
)

replace github.com/vuka-lang/vuka/v2 => ../../..

replace github.com/vuka-lang/web/v2 => ./webretired

replace vukatest.local/islandui => ./islandui

replace vukatest.local/stateui => ./stateui

replace vukatest.local/webstub => ./webstub

replace vukatest.local/orm => ./orm

replace vukatest.local/ui => ./ui
