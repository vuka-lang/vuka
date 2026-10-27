# LLMs & AI Assistants

[[toc]]

## Introduction

Vuka is newer than what most language models were trained on, so an
assistant writing Vuka code guesses unless it reads these docs. The site
publishes them in forms a model reads well, and the `vuka` command answers
the questions an assistant would otherwise guess at.

## The Docs as Text {#files}

| URL | What it holds |
|---|---|
| [`/llms.txt`](https://vuka-lang.github.io/llms.txt) | An index: every page in the sidebar's order, a line on each, its Markdown URL |
| [`/llms-full.txt`](https://vuka-lang.github.io/llms-full.txt) | Every page in one file, for tools that take a whole corpus |
| `/<page>.md` | Any page as Markdown: [`/web/graphql.md`](https://vuka-lang.github.io/web/graphql.md) next to [`/web/graphql`](/web/graphql) |

They are built with the site, so they match the docs you read here. The
Markdown is the page's source with the site's markup taken out: samples
that differ between `.vuka` and plain Go keep both, and links are absolute.

Give an assistant `llms.txt` and let it open the pages a task needs; give it
`llms-full.txt` when it can hold the whole thing and you would rather it
didn't fetch.

## Pointing an Assistant at Them {#setup}

Put the index in the instructions your assistant reads for the project:
`CLAUDE.md` for Claude Code, `AGENTS.md` for tools that read that, or your
editor's rules file. A starting point:

```markdown
This app is written in Vuka (https://vuka-lang.github.io): .vuka files and
.vuka.html templates transpile to plain Go.

- Docs index: https://vuka-lang.github.io/llms.txt — read the page for the
  feature before writing code that uses it (forms, GraphQL, live pages,
  folder routes, the ORM).
- Build and check with `vuka build ./...` and `vuka vet ./...`, never plain
  `go build`: the go command doesn't read .vuka files.
- For an error code (E0150, W0801), run `vuka explain <code>`.
- Don't edit generated Go; change the .vuka or .vuka.html source.
```

An assistant that can fetch URLs opens the pages itself. One that can't
works from what you paste: a page's `.md` URL gives the text without the
site around it.

## The Command Is the Reference {#cli}

What the docs describe, the `vuka` command checks. An assistant gets
further running it than reasoning about code it can't build:

| Command | What it tells an assistant |
|---|---|
| `vuka build ./...` | whether the app compiles, with each error at its source line and, where the cause is certain, the fix |
| `vuka vet ./...` | every error and warning, the ones a build stops before |
| `vuka explain E0201` | an error code's page, offline |
| `vuka explain file.vuka` | the Go a `.vuka` file becomes |
| `vuka routes` | every route: method, path, page, file and line, guards, layouts |
| `vuka graphql schema` | the GraphQL schema the app serves |
| `vuka doctor` | whether Go, Node and the app's setup are what Vuka needs |

Diagnostics name the source position in your `.vuka` and `.vuka.html`
files, never a line of generated Go, so an assistant fixes the file you
wrote.

## Keeping It Current {#current}

The files follow the docs on the site, which follow the latest release. An
app on an older version has its own docs in the module cache: the `docs`
folder of the `github.com/vuka-lang/vuka` version its `go.mod` requires.
