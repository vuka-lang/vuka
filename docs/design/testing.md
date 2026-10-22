# Testing

What runs, where, and what happens when a test fails for a reason other
than a bug.

## What CI runs

Every push and pull request, in each repository (`.github/workflows/ci.yml`):

| Repository | Job | What |
| --- | --- | --- |
| vuka | lib | gofmt, `go vet ./...`, `go test -race ./...` |
| vuka | cmd | `go vet` and `go test` of `cmd/vuka` (but `TestConformance`) against `main` of ui and web, with gopls, templ, Tailwind, Chrome and the pinned `node_modules` of `cmd/vuka/testdata/node` — the real-Vite and Chrome end-to-end suites included |
| vuka | conformance | `TestConformance` (cmd/vuka): every app of `examples/conformance`, written the one way, built by `vuka build` and ejected to Go, with and without the generated speed-ups, answering alike ([library.md §8](library.md#_8-conformance)); with ui, web and orm at `main` (or the same branch), Tailwind and the islands' `node_modules` |
| vuka | fuzz | `FuzzPackage`, `FuzzSyntax` (transpile) and `FuzzSource` (formatter), 30 seconds each |
| ui | test | gofmt, `go vet ./...`, `go test -race ./...`, the demos checked against the vuka command built from `main` |
| ui | fuzz | `FuzzTreePatch` (live diffs), 30 seconds |
| web | test | gofmt, `go vet ./...`, `go test -race ./...`: the node runtime tests, Chrome |

CI sets `CI`, and a test that needs a tool, a checkout or a `node_modules`
fails without it there instead of skipping (`needs` in cmd/vuka, the demos'
`needs` in ui, `testenv.Needs` in web). Locally it skips and says what to
install. Nothing is allow-listed: a red job is a red build.

## Running the suites locally

- cmd/vuka's tests build apps against checkouts of ui and web beside the
  vuka checkout (`../vuka-ui`, `../vuka-web`), or `$VUKA_UI` and `$VUKA_WEB`.
  Run them with `GOWORK=off`: they mean what CI means only against the
  versions the modules pin.
- The end-to-end `node_modules` are pinned by lockfile:

  ```sh
  cd cmd/vuka
  npm ci --prefix testdata/node/vite      # VUKA_VITE_NM: Vite, Vue, Nuxt UI
  npm ci --prefix testdata/node/islands   # VUKA_ISLAND_NM, VUKA_TS_NM: esbuild, the frameworks, TypeScript
  ```

  The tests find them there; the environment variables still win. The
  Vite plugin comes from `vite/vuka.js` of the web checkout, or `$VUKA_PLUGIN`.
- ui's demo tests need the vuka command on `PATH` or in `$VUKA`.
- Chrome is found in the usual places, or `$VUKA_CHROME`.

## Fuzzing

Each fuzzer is seeded with the golden cases. Run one longer when touching
what it covers:

```sh
go test -run '^$' -fuzz '^FuzzPackage$' -fuzztime 5m ./transpile
```

A failing input lands in `testdata/fuzz/<Fuzzer>/`; commit it with the fix,
so it runs as a regression case in every `go test`.

## Flaky tests

A test that fails without a code change is a bug — in the code (a race, an
ordering the API doesn't promise) or in the test (a sleep standing in for a
condition, a shared global, a port). Fix it; until then:

- **Never retry silently.** No re-run-until-green in CI, no `-count`
  passes until one succeeds, no retry wrappers. A retry hides the race
  users will hit.
- **Quarantine, visibly.** When a fix can't land the same day, skip the
  test with its owner and the date it was quarantined, and a link to the
  issue:

  ```go
  t.Skip("quarantined 2026-10-19 by @paulmanoni: races the hub's subscribe, #123")
  ```

  `grep -rn quarantined` lists them all. A quarantine older than two weeks
  is fixed or the test is deleted, with the reason in the commit.
- **Reproduce under load.** Most flakes show with `go test -race -count=50
  -run TestX`, or with several copies of the package's test binary running
  at once. Wait on conditions, never on time; poll with a deadline.
