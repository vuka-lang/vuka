# Mail

A welcome email on sign-up, a password reset link, a receipt: inject a
`*mail.Mailer` and send a `mail.Message`. The body is a JSX or templ
component, so an email is written like a page; the plain-text part is made
from it.

```vuka
import "github.com/vuka-lang/web/mail"

// WelcomeEmail is the welcome email's HTML.
func WelcomeEmail(name string, link string) ui.Node {
	return <div style="font-family: system-ui, sans-serif">
		<h1>Welcome, {name}</h1>
		<p>Thanks for signing up. Confirm your address within a day:</p>
		<p><a href={link}>Confirm my address</a></p>
	</div>
}

@web.Post("/signup")
func SignUp(ctx context.Context, in Signup, owners *Owners, m *mail.Mailer) (web.Redirect, error) {
	ow := owners.Add(in.Name, in.Email)
	err := m.Send(ctx, mail.Message{
		To:      []string{in.Name + " <" + in.Email + ">"},
		Subject: "Welcome to Pets",
		HTML:    <WelcomeEmail name={in.Name} link={confirmLink(ow)} />,
	})
	if err != nil {
		return "", err
	}
	return web.Redirect("/signup/sent"), nil
}

func main() {
	app := web.New(mail.Provide())   // the Mailer, configured from the environment
	app.Run("")
}
```

`mail` is a package of its own: an app that sends no email doesn't link it.

**From a live page's route**, send once: the route runs again when the
page's socket joins, so guard the send with `web.IsLiveJoin` (see
[Side effects in a live page's handler](/web/live#side-effects-in-a-live-page-s-handler)):

```vuka
if !web.IsLiveJoin(ctx) {
	if err := m.Send(ctx, welcome); err != nil {
		return nil, err
	}
}
```

A mail sent from an event handler goes once per event, needing no guard.

## Configuration

`mail.Provide()` builds the Mailer from the environment, or from the
[`.env` files](/web/configuration) `web.New` reads:

```sh
MAIL_DRIVER=smtp                       # log (the default) or smtp
MAIL_HOST=smtp.example.com
MAIL_PORT=587                          # default: 587; 465 with tls; 25 with none
MAIL_USERNAME=pets
MAIL_PASSWORD=change-me
MAIL_ENCRYPTION=starttls               # starttls (the default), tls or none
MAIL_FROM=Pets <hello@pets.example>    # the sender when a message names none
MAIL_FROM_NAME=Pets                    # its name, when MAIL_FROM has none
```

| `MAIL_ENCRYPTION` | |
|---|---|
| `starttls` | connect in plain text and upgrade; a server that can't is an error, never a fallback |
| `tls` | TLS from the first byte (port 465) |
| `none` | no TLS, for a mail catcher on your machine; the password is sent only to `localhost` |

With the **log** driver — the default, so development and tests never send
real mail by accident — `Send` logs each message (sender, recipients,
subject, text) and sends nothing. A driver, encryption or address that
doesn't make sense fails `app.Start`, naming the variable.

In code, `mail.Provide(mail.Config{…})` takes the same settings, and more:

| `mail.Config` | |
|---|---|
| `Driver`, `Host`, `Port`, `Username`, `Password`, `Encryption`, `From`, `FromName` | as above |
| `TLS` | a `*tls.Config`: a private CA, a client certificate |
| `Timeout` | dialing the server, and a send without a deadline of its own (six times as long); 10s |
| `LocalName` | the name given in `EHLO`; `localhost` |
| `Transport` | sends instead of the driver: an email API over HTTP, say |
| `Retries`, `Backoff` | `SendLater`'s retries (3) and first wait (2s, doubling) |
| `Logger` | the log driver's and `SendLater`'s; `slog`'s default |

`mail.New(cfg)` makes a Mailer outside an app; `mail.ConfigFromEnv()` reads
the variables.

## Messages

```vuka
m.Send(ctx, mail.Message{
	From:    "Pets Support <help@pets.example>",   // else MAIL_FROM
	To:      []string{"Ada Lovelace <ada@example.com>"},
	Cc:      []string{"owner@example.com"},
	Bcc:     []string{"audit@pets.example"},        // receives it, isn't listed
	ReplyTo: "help@pets.example",
	Subject: "Your receipt",
	HTML:    <Receipt order={o} />,
	Text:    "",                                    // made from the HTML when empty
	Headers: map[string]string{"List-Unsubscribe": "<https://pets.example/unsubscribe>"},
	Attachments: []mail.Attachment{
		{Filename: "receipt.pdf", Data: pdf},       // content type from the extension
	},
})
```

`HTML` is a `ui.Node`: a JSX element, a templ component, or
`ui.Safe(html)` for HTML you have as a string. It renders with `Send`'s
context. The text part, when `Text` is empty, is the HTML's text:
paragraphs and list items on lines of their own, a link followed by its
address, scripts and styles left out.

The message is built for you: text and HTML as `multipart/alternative`,
attachments around them in `multipart/mixed` (base64), bodies in
quoted-printable UTF-8, names and subjects with other characters than
ASCII encoded, a `Date` and a `Message-ID`.

**Refused before sending.** A message with no recipient, an address that
doesn't parse, or a line break in any header — a subject, a name, a header
value, a file name — is an error, `*mail.InvalidError`, and nothing is sent.
A line break is how a header injection smuggles in a `Bcc:`, so input from
a form can go into a subject safely. Tell the visitor:

```vuka
var invalid *mail.InvalidError
if errors.As(err, &invalid) {
	return "", web.BadRequest(invalid.Reason)   // To: "x" isn't an email address
}
```

## Sending later

`Send` waits for the server. `m.SendLater(ctx, msg)` returns at once: it
checks and renders the message now — an invalid message is still an error
here — and sends it in the background. A temporary failure (a `4xx`
answer, a connection that fails) is retried three times, after 2, 4 and 8
seconds; a permanent one (`5xx`) is not, and the last failure is logged.
The app's `Stop` waits for the messages still queued, within its context.

The queue is in memory: messages queued when the process dies are lost.
For mail that must go out, send it from a job of your own that records it,
or keep using `Send`.

## In the toolbar

In development the [toolbar](/web/toolbar)'s **Mail** panel lists the
messages a request — or a [cron](/web/crons) run — sent or queued: sender,
recipients, subject, attachments, whether it was sent, logged or refused
and why, and the HTML in a preview (sandboxed: its scripts don't run), with
the text part beneath. Under the log driver that is where development
email goes.

![The Mail panel](/toolbar-mail.png)

## Testing

The log driver keeps what it was given: `m.Sent()` returns each message
with its rendered `HTML` and `Text`, its `Recipients` (Bcc included) and
the `Raw` MIME message. Package `mailtest` has a quiet log-driver Mailer
and helpers:

```vuka
func TestSignup(t *testing.T) {
	m := mailtest.New()   // logs nothing; sender test@example.com
	app := web.New(web.Provide(OpenDB, m))
	srv := httptest.NewServer(app.Handler())
	defer srv.Close()

	http.PostForm(srv.URL+"/signup", url.Values{"name": {"Ada"}, "email": {"ada@example.com"}})

	msg := mailtest.Last(t, m)   // fails the test when nothing was sent
	if msg.Recipients[0] != "ada@example.com" || !strings.Contains(msg.Text, "Welcome, Ada") {
		t.Errorf("the welcome email: %+v", msg)
	}
}
```

| | |
|---|---|
| `mailtest.New(cfg…)` | a log-driver Mailer to provide |
| `mailtest.Sent(t, m)` | every message kept, after `SendLater`'s are sent |
| `mailtest.Last(t, m)` | the last one |
| `mailtest.To(t, m, addr)` | those that went to `addr` |
| `m.Reset()` | forget them |
