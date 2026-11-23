# vuka — guide for coding agents

vuka is a Go transpiler and CLI: plain Go + `.vuka.html` templates are the
language, `.vuka` is sugar. This repo holds the transpiler (`transpile/`),
the toolchain (`toolchain/`), the docs site (`docs/`), the editor grammars
(`editors/`) and the CLI (`cmd/vuka`, its own module). Sibling repos `ui`,
`web`, `orm` and `flow` complete the set and release on one version.

## Design philosophy (how features are designed here)

- **No new keyword, type, tag or annotation unless necessary.** Before
  adding one, exhaust inference, an existing convention, or classification
  by use. Precedents to imitate: the one-state merge classified fields by
  how they are used instead of adding `@ui.Local`/`@ui.Derived` and retired
  `ui.Assign`; §5 fallthrough deleted `Attrs`/`Class` declarations instead
  of adding routing syntax; a proposed `ui.Dialog` type was rejected for
  `ui.State[*T]` plus the kit. The one reason to require a declaration is a
  **trust boundary** (`ui.State` = declared client writability): infer what
  costs performance, declare what costs safety.
- **Stay close to the frontend-framework writing feel.** A developer
  arriving from Vue, React or Svelte should find their spelling works or
  has one obvious vuka form. Study how those frameworks spell a feature
  before inventing one (fallthrough ← Vue, `asChild` ← Radix, positional
  ids ← React's useId, `Teleport`, `Await`/`@ui.Defer` ← Suspense/Phoenix,
  `keep` ← KeepAlive) — then make it compile-time-checked where they fail
  at runtime. The error message with a did-you-mean is part of the feature.
- **Design before code.** Component-system design lives in
  `docs/rfc/components-seamless.md`; amend the RFC first, then build in
  the RFC's slices. Check what already exists before proposing (Teleport,
  Await, slot fallbacks were all proposed twice).
- **Compatibility promise.** Existing call sites keep compiling; a change
  of observed semantics rides a release note, never a patch.

## Module and CI facts that bite

- The **root module has zero dependencies** — never add a require. ui/web
  are wired into CI by repo checkout, not go.mod; local runs set `VUKA_UI`
  and `VUKA_WEB` to sibling checkouts.
- **cmd/vuka is its own module**: build and test from inside `cmd/vuka/`.
  When it starts importing a NEW root-module symbol, bump its parent
  require (`GOWORK=off GOFLAGS=-mod=mod GOPROXY=direct GOSUMDB=off go get
  github.com/vuka-lang/vuka/v2@<sha>`) or the post-merge
  `go install @main` CI check breaks.
- **Same-named branches across the vuka-lang repos are swapped into each
  other's CI builds** — coordinate a cross-repo change by pushing the same
  branch name everywhere. On main, newer pushes supersede-cancel older
  runs: a `cancelled` conclusion is not a failure; judge by the latest run.
- cmd/vuka fixture go.mods are settled by `settleModule`
  (`cmd/vuka/testmod_test.go`) when a dependency bump raises `x/*`
  versions — rerun, never hand-edit fixtures.

## Gates (run before pushing)

- `GOWORK=off go test ./docs/...` with `VUKA_UI`/`VUKA_WEB` set — docs
  fences are live: ` ```vuka ` samples are grammar-tokenized and every
  element must close; ` ```go ` samples vet against the published ui, so a
  proposed API's sample uses a vuka or html fence.
- `node editors/vscode/test/grammar.test.mjs` for anything touching
  grammars or docs fences.
- New error/warning codes take unused gaps in the E/W ranges (confirm via
  git history that a code was never used); every code gets its docs page
  (`TestCodesUsed`).

## House rules

- Never use `git stash` — the stash list is shared across worktrees and
  sessions. Work in worktrees under `.claude/worktrees/`.
- Generic domains only (users, orders, cards, Button) in code, tests,
  fixtures, commits and PRs — never a downstream app's or customer's name.
- Never wipe the Go module cache; when disk is tight, prune the build
  cache by age.
- Tag or release only on the owner's explicit order; the set (vuka, ui,
  web, orm, flow) moves on one version together.
