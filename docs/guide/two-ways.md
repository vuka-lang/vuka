# Source Files

[[toc]]

## Introduction

A Vuka package can hold three kinds of source file, and you use one file
type for each job. All three share package scope, so code in one kind of
file calls code in another directly.

## Choosing a File Type

The table below shows which file type does which job:

| File | Job |
|---|---|
| `.vuka.html` | Pages, layouts, components and email markup; one struct in a `<script lang="go">`, then markup |
| `.go` | Ordinary Go logic, services, models, API handlers and startup |
| `.vuka` | Logic using Result, Option, match, enums, overloading or decorators |

## Shape Files

Rendering lives in shape files. A `<script lang="go">` holds the Go, and the
markup follows it.

In the script's struct, exported fields are props, unexported fields are
server state or dependencies, and `ui.State[T]` is browser state written by
assignment in event attributes. See [Templates](/ui/templates).

## Page Routes

Page routes come from `pages/`, bracket names and `_layout.vuka.html`.
`pages/route.go` is generated; main constructs `web.New(pages.Routes(), …)`.
See [Getting started](/web/getting-started).
