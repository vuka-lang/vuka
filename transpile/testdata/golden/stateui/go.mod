// stateui is a JSX target fixture for the client-state golden cases: it adds
// the state API (State, Derived, Show, …) over github.com/vuka-lang/ui/v2,
// rendering what the compiler wrote. The golden module replaces it to this
// directory.
module vukatest.local/stateui

go 1.25.0

require (
	github.com/vuka-lang/ui/v2 v2.0.0
	github.com/vuka-lang/vuka/v2 v2.0.0
)
