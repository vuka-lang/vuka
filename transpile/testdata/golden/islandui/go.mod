// islandui is a JSX target fixture for the island golden cases: it adds the
// island API over github.com/vuka-lang/ui so those cases need no new ui
// version. The golden module replaces it to this directory.
module vukatest.local/islandui

go 1.25.0

require (
	github.com/vuka-lang/ui v0.1.0
	github.com/vuka-lang/vuka v0.10.0
)
