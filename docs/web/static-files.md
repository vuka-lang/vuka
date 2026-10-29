# Static Files

[[toc]]

## Introduction

Images, fonts and stylesheets the app ships are served by `app.Static` from
an embedded directory. Link them with `web.Asset`, giving the path the file
is served at:

```html
<img src={web.Asset("/static/images/logo.png")} alt="Logo" />
<link rel="stylesheet" href={web.Asset("/static/site.css")} />
```

- `vuka build` and `vuka vet` check the path: a file that isn't there is an
  error, with the nearest file as a suggestion.
- In the binary `vuka build` makes, the link is the file's content-hashed
  name, `/static/images/logo.3f9a1c2b.png`, which the browser keeps for good
  (`Cache-Control: public, max-age=31536000, immutable`). A changed file
  gets a new name, so a deploy never serves a stale copy.
- Under `vuka run` the link is the path as written, revalidated on each use,
  so an edited file shows on the next reload.

## Mounting a Directory

```go
//go:embed static
var static embed.FS

func main() {
	app := web.New()
	assets, _ := fs.Sub(static, "static")
	app.Static("/static", assets)
	log.Fatal(app.Run(""))
}
```

`vuka build` finds the mounts in the source: `app.Static` with a literal
prefix, given an `embed.FS` variable or `fs.Sub` of one with a literal
directory (in the call, or in the variable the call names). It hashes their
files and embeds the names in the binary. A mount it can't read that way —
`os.DirFS("uploads")`, a file system built in another function — is served
as it is: its files are neither hashed nor checked, and `web.Asset` links
them by their plain path.

The plain name keeps working beside the hashed one, so a link from outside
the app (an old email, a bookmark) still finds the file.

## In Markup and in Go

In markup the context is implicit: `{web.Asset("/static/x.png")}`. Go code
passes it:

<!-- go:fragment -->
```go
logo := web.Asset(ctx, "/static/images/logo.png")
_ = logo
```

A path built at run time is not checked at build time; at run time a path no
mount serves is linked as written, and a warning is logged once.

When the app is mounted under a path (`http.StripPrefix("/shop", app)`),
the links carry it: `/shop/static/images/logo.3f9a1c2b.png`.

## Absolute URLs for Emails

An email needs the scheme and the host. `web.AssetURL` adds the request's:

<!-- go:fragment -->
```go
logo := web.AssetURL(ctx, "/static/images/logo.png")
_ = logo // https://example.com/static/images/logo.3f9a1c2b.png
```

Without a request (a cron, a job) it uses the app's public URL:

<!-- go:fragment -->
```go
app := web.New(web.BaseURL("https://example.com"))
_ = app
```

## Stylesheets

`url(…)`s in the stylesheets a mount serves name the hashed files, whether
absolute (`url(/static/images/bg.png)`) or relative to the stylesheet
(`url(images/bg.png)`); so do those of the app's [Tailwind CSS](/web/styling).
A stylesheet's own hash covers the files it links, so a new image gives it a
new name too.

## Moving Existing Links

`vuka vet` warns on a path written by hand in a `src`, `href`, `srcset`,
`poster` or `data` attribute under a mount ([W0404](/errors/W0404)). The
editor's quick fix rewrites one; `vuka fix static-paths` rewrites a module:

```sh
vuka fix static-paths
```

```html
<!-- before -->
<img src="/static/images/logo.png" alt="Logo" />
<!-- after -->
<img src={web.Asset("/static/images/logo.png")} alt="Logo" />
```

## In the Editor

Inside `web.Asset("…")` the editor completes the mounted files' paths, shows
a file's size (and an image's dimensions) on hover, and opens the file on
go-to-definition.
