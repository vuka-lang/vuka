# Mail

[[toc]]

## Introduction

Mail sending is a service, and email markup is a `.vuka.html` shape. You
keep transport configuration and sending logic in `.go` or `.vuka` files.

This page shows an email shape, how to configure delivery, what a message
holds, and how to test what the app sends.

## Writing an Email

Save this as `emails/receipt.vuka.html`:

```html
<script lang="go">
package emails

type Receipt struct {
    OrderID int
    Customer string
}
</script>

<main>
  <h1>Order {OrderID}</h1>
  <p>Thank you, {Customer}.</p>
</main>
```

Render `emails.Receipt{OrderID: id, Customer: name}` with `ui.String` when
constructing the message body. This uses the actual generated shape type.

Register the mail service's constructor with `app.Service(NewMailer)`; see
[Dependency Injection](/web/dependency-injection).

## Configuring Delivery {#configuration}

Provide SMTP credentials through environment variables, not source or
browser props; see [Configuration](/web/configuration). Use verified sender
addresses and transport security suitable for the provider.

Fail startup when required configuration is absent.

## Composing Messages {#messages}

A message has a sender, recipients, a subject and text and HTML bodies.
Supply a useful plain-text alternative, and escape visitor content through
the renderer.

Treat delivery failure as a service error rather than silently reporting
success to the visitor.

> [!WARNING]
> Avoid putting tokens in logs.

## Images in Emails {#images}

A mail client fetches an email's images from the app, so they need
absolute URLs. `web.AssetURL` gives one for a file an `app.Static` mount
serves, content-hashed and cached for good like a page's
[`web.Asset`](/web/static-files):

::: code-group

```vuka-html [Vuka]
<script lang="go">
package emails

import "github.com/vuka-lang/web/v2"

type Receipt struct {
    OrderID int
}
</script>

<main>
  <img src={web.AssetURL("/static/images/logo.png")} alt="Shop" width="120" />
  <h1>Order {OrderID}</h1>
</main>
```

```go [Generated Go]
func (r Receipt) View() ui.Node {
	return ui.Ctx(func(ctx context.Context) ui.Node {
		return ui.HTML(`
<main>
  <img src={web.AssetURL(ctx, "/static/images/logo.png")} alt="Shop" width="120"/>
  <h1>Order {r.OrderID}</h1>
</main>`, web.AssetURL(ctx, "/static/images/logo.png"), r.OrderID)
	})
}

// …
```

:::

Rendered with the request's context (`ui.String(ctx, …)` in a handler), the
URL takes the request's scheme and host. A cron or a job has no request, so
the app names its public URL with `web.BaseURL("https://shop.example.com")`;
see [Absolute URLs for Emails](/web/static-files#absolute-urls-for-emails).

## Sending Later

Queue slow delivery outside a latency-sensitive request where appropriate.
Retries need idempotency, so that a repeated submission does not send
duplicates. Ensure background work participates in the app lifecycle and
shutdown.

## Testing Mail {#testing}

`mailtest.New()` captures messages instead of sending them, and
`mailtest.Last(t, mailer)` inspects the latest one. Assert recipients,
subject, text and rendered HTML. Use fake delivery services in request
tests.

## In the Toolbar

Development mail diagnostics in the [Development Toolbar](/web/toolbar) help
inspect delivery without production SMTP.

> [!WARNING]
> Never expose them publicly or log secret links in production.
