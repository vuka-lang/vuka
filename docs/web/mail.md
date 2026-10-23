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
package emails

type Receipt struct {
    OrderID int
    Customer string
}
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
