// stateui is a JSX target fixture for the client-state golden cases: it adds
// the state API (State, Derived, Bind, Show, …) over github.com/vuka-lang/ui
// so those cases need no new ui version. The golden module replaces it to
// this directory.
module vukatest.local/stateui

go 1.25.0

require github.com/vuka-lang/ui v0.1.0
