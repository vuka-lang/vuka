# Mail

Mail sending is a service; email markup is a `.vuka.html` shape. Keep transport
configuration and sending logic in `.go` or `.vuka` files.

Save as `emails/receipt.vuka.html`:

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
Register the mail service's constructor with `app.Service(NewMailer)`.

## Configuration

Provide SMTP credentials through environment variables, not source or browser
props. Use verified sender addresses and transport security suitable for the
provider. Fail startup when required configuration is absent.

## Messages

A message has sender, recipients, subject and text/HTML bodies. Supply a useful
plain-text alternative, escape visitor content through the renderer and avoid
putting tokens in logs. Treat delivery failure as a service error rather than
silently reporting success to the visitor.

## Sending later

Queue slow delivery outside a latency-sensitive request where appropriate.
Retries need idempotency so a repeated submission does not send duplicates.
Ensure background work participates in the app lifecycle and shutdown.

## Testing

`mailtest.New()` captures messages instead of sending them;
`mailtest.Last(t, mailer)` inspects the latest one. Assert recipients, subject,
text and rendered HTML. Use fake delivery services in request tests.

## In the toolbar

Development mail diagnostics help inspect delivery without production SMTP.
Never expose them publicly or log secret links in production.
