# Routing

[[toc]]

## Introduction

You declare your application's pages as files under `pages/`, not with
registration calls. The folder tree gives each page its URL, and the compiler
turns the tree into a generated routing table.

This page covers the basics. [Folder Routes](/web/folder-routes) goes further,
into brackets, catch-alls and layouts.

## Basic Routing

Each `.vuka.html` file under `pages/` is a page, and its path is its URL. The
table below shows how files map to URLs:

| File | URL |
|---|---|
| `pages/index.vuka.html` | `/` |
| `pages/pets/index.vuka.html` | `/pets` |
| `pages/pets/[id].vuka.html` | `/pets/{id}` |
| `pages/files/[path...].vuka.html` | `/files/{path...}` |
| `pages/_layout.vuka.html` | Wraps descendants; no URL |

`pages/route.go` is generated from the tree. Your `main.go` starts the app
with `web.New(pages.Routes(), …)`.

> [!WARNING]
> Don't edit `pages/route.go` by hand. Change the files under `pages/` and
> let the compiler write it again.

### Page Parameters

A page's exported fields bind the request's path values first, then its query
values. Their Go types and validation decide which input a page accepts. See
[Parameters & Validation](/web/parameters) for the details.

## API Routes

Endpoint functions under `pages/api/` return JSON or typed errors. Their input
structs bind form and JSON fields, and services come from the container.

For an API outside the folder tree, the ordinary `app.Get`, `app.Post` and
other HTTP registration methods remain available in Go. Keep page rendering in
shape files and page routes in the tree.

## Forms That Fail

A validation failure carries field errors and status 422. Your handler should
keep the safe submitted values and render feedback through its page state.

> [!WARNING]
> Never use an unvalidated browser value as the authority for an action.

## Services

You register constructors with `app.Service(NewX)` before `Build` or `Run`. A
constructor's arguments are its dependencies, and hook and handler parameters
are supplied by type.

A dependency that can't be filled, or an ambiguous body binding, is a startup
error with source information. See [Dependency Injection](/web/dependency-injection)
and [Parameters & Validation](/web/parameters).
