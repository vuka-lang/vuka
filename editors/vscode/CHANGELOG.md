# Changelog

## 0.7.0

- Require a Vuka v2.0.0 language server and offer `go install github.com/vuka-lang/vuka/cmd/vuka/v2@latest` when an older one is found.

- Highlight Vuka 2.0 `.vuka.html` shapes: Go headers, decorator hooks and field attributes, followed by markup with Go statements and browser-state assignments.
- Keep method bodies, channel receives, comments and raw strings in the Go header until the top-level markup boundary.
- Offer live/static shape, folder layout, island page and Mount hook snippets in Vuka HTML; API method-function snippets remain in Vuka logic files.
- Complete closing tags only in `.vuka.html` markup and stop advertising retired JSX and page decorators.
- Cover bracket route filenames, Go/markup boundaries, embedded TypeScript/CSS and the new snippets in regression tests.

## 0.6.0

- Show the language server version and path in `.vuka.html` as well as `.vuka` files.
- Warn when the server predates Vuka v1.4.0 and cannot resolve implicit template `ctx`.
- Offer a copyable server update command and explain how to restart the server.

## 0.5.0

- Go, Vuka and template language support through the Vuka language server.
