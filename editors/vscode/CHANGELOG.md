# Changelog

## 0.9.0

- Declare the `vukaContextVar` semantic token type, a context processor's variable read by name (`user`, `request`), and the TextMate scopes it falls back to (Vuka v2.4.0).

## 0.8.0

- Highlight the `<script lang="go">` shape form (Vuka v2.3.0): the script's content as Go, its end the `</script>` outside a Go string or comment, the markup after it; the shape snippets write that form, and closing-tag completion starts after the script. With a Vuka v2.3.0 language server the script folds as one, the whole file formats (`vuka fmt`), and a shape in the Go-header form offers **Write the Go in &lt;script lang="go"&gt;**.
- Make Tailwind CSS IntelliSense work in shapes: `.vuka.html` and `.vuka` read as HTML, and classes found in `class="…"`, the strings of `class={…}` (concatenations too) and Go helpers named `…Class`/`…Classes`, their calls and bodies.
- Turn Emmet on in `.vuka.html` files (`emmet.includeLanguages`); `.vuka` files stay without it.
- With a Vuka v2.3.0 language server: the shape's struct holds its markup in the outline, so sticky scroll keeps it pinned over the markup; the Go header folds as one; Format Selection formats only the selected lines; ARIA attributes, roles and their values, per-element event handlers, `ref` and `ui.Ref("…")` names complete; a loop's first element completes with its `key`.
- Cover the configuration defaults, the class regexes and the header's Go scopes in tests.
- Declare the Vuka semantic token types of `.vuka.html` markup (`vukaProp`, `vukaState`, `vukaBrowserState`, `vukaEvent`, `vukaComponent`, `vukaLocal`) and the TextMate scopes they fall back to, so themes colour props, server state, browser state, events, components and template locals apart.
- Run **Vuka: Show Routes** from a page's code lens: the route's entry and the module's `vuka routes` table in the Vuka Routes output.
- With a Vuka v2.3.0 language server: **Extract component…** on a selection of a shape's markup asks the new component's name (`vuka.extractComponent`, kept out of the command palette); moving a shape's methods to its `.vuka` file and the quick fixes are the server's code actions.
- With a Vuka v2.3.0 language server: hover on holes and `on…` attributes, component attributes with required ones marked, code lenses above pages, `urls` chains and `href`s as links, and page renames under `pages/` updating `urls` chains (`workspace/willRenameFiles`).

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
