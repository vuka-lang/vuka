A button, or something that looks like one: six variants, four sizes, and a click that runs in the browser or on the server.

## Usage

Import the package `vuka ui add` wrote the component into, then write the tag:

<!-- code usage -->

## Clicks

`onClick` runs in the browser: it takes statements writing
[browser state](/ui/state), calls among them, or a `ui.JS` chain, and needs
no round trip. `on` takes a live handler or a `ui.JS` chain. When both are
set, `onClick` wins.

<!-- code click -->

## Size

`size` takes `SizeSm`, `SizeLg` or `SizeIcon`; leave it out for the default.

<!-- example size -->

## Default

The default variant fills the button with the primary colour.

<!-- example default -->

## Outline

<!-- example outline -->

## Secondary

<!-- example secondary -->

## Ghost

<!-- example ghost -->

## Destructive

For an action that deletes or cannot be undone.

<!-- example destructive -->

## Link

Drawn as a link, with an underline on hover.

<!-- example link -->

## Icon

`SizeIcon` makes a square button for an icon alone. Give it an `aria-label`
through `attrs`, since it has no text.

<!-- example icon -->

## With Icon

An `<svg>` among the children is sized and spaced for you: the button
leaves it at the size of the text and pulls in its padding on that side.

<!-- example with_icon -->

## Rounded

`class` is merged last, so it wins over the component's own classes:
`rounded-full` makes a round button.

<!-- example rounded -->

## Spinner

Show work in progress with a spinning icon and `disabled`, so the button
cannot be pressed twice.

<!-- example spinner -->

## RTL

The button follows the text direction of the page: in a right-to-left
block its content reads from the right, and a directional icon is flipped
with `rotate-180`.

<!-- example rtl -->

<!-- related badge card dialog dropdown-menu -->
