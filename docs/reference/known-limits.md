# Known Limits

These are the things Vuka doesn't do yet, or does differently from what you may
expect. Each one is an error at compile time or a documented difference, not a
silent surprise. The README repeats this list; a test keeps the two the same.

## The List

- Overloads are resolved within a package; calling another package's overloads
  goes through its `@export` names. See [Overloading](/features/overloading).
- Generic functions can't be overloaded yet ([E0120](/errors/E0120)).
- Go's own error messages, outside the editor, show the overloads' Go names (`areaInt`).
- An overloaded method can't satisfy an interface method of the plain name.
- `?` works anywhere an expression does, with three exceptions, each
  [E0110](/errors/E0110): a `case` of a `switch` or `select`, a `for` loop's post
  statement, and markup. See [The ? Operator](/features/try).
- Struct patterns of generic types aren't supported yet. See [match](/features/match).
