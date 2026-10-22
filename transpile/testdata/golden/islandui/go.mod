// islandui is a JSX target fixture for the island golden cases: it adds the
// island API over github.com/vuka-lang/ui/v2 so those cases need no new ui
// version. The golden module replaces it to this directory.
module vukatest.local/islandui

go 1.25.0

require (
	github.com/vuka-lang/ui/v2 v2.0.0-20261022221307-3737266aaf1e
	github.com/vuka-lang/vuka/v2 v2.0.0
)
