package transpile

// The goldens of the JSX lowering, which a .vuka.html file's markup
// compiles through, are written as JSX in .vuka files: the tests reach it
// so; an app's .vuka file holding JSX is E0151.
func init() { jsxInVuka = true }

// SetJSXInVuka sets whether a .vuka file may hold JSX, returning what it was.
func SetJSXInVuka(on bool) (was bool) {
	was, jsxInVuka = jsxInVuka, on
	return was
}
