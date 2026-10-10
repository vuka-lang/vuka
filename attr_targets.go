package vuka

// Targets restricts where an attribute type may be written, as Java's
// @Target does. Written on the type, it makes any other use a compile error
// at the attribute:
//
//	@vuka.Targets(vuka.OnField)
//	type Char struct{ Max int }
//
// Go code declares the same with a method whose result's length is the
// targets: func (Char) vukaTargets() [vuka.OnField]struct{} { return {} }.
type Targets uint16

const (
	OnFunc   Targets = 1 << iota // a function declaration
	OnMethod                     // a method declaration
	OnType                       // a type declaration
	OnField                      // a struct field
	OnParam                      // a function's or method's parameter
	OnVar                        // a var or const declaration
)
