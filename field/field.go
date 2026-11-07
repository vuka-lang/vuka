// Package field is the vocabulary a struct field's attributes describe it to
// a form with. A library's field attributes implement Describer, and those
// needing the server to check a value Checker; a form library reads them
// (vuka.FieldAttrs) without knowing the library that declared them, so a
// model's attributes (orm.Char{Max: 100}) describe its form fields as a
// form's own (ui.Placeholder("e.g. Desk lamp")) do.
//
//	type Max int
//
//	func (m Max) Describe(s *field.Spec) { s.Max = field.Float(float64(m)) }
package field

import "context"

// Spec is what a field's attributes say of it as a form field. A
// Describer sets the members it knows and leaves the others alone.
type Spec struct {
	Label, Help, Placeholder string
	// Pattern is a regular expression the whole value matches.
	Pattern string
	// Required makes the field required; Optional makes a field whose Go
	// type would be required optional (a column that may be NULL). On a
	// bool, Required means it must be ticked.
	Required, Optional bool
	// Email checks the value is an email address.
	Email bool
	// Min and Max bound a text's length, or a number's value.
	Min, Max *float64
	// Choices are the values the field may take: a select's options.
	Choices []Choice
	// Widget is its control: "textarea", "radio", "password", "email", …
	Widget       string
	Autocomplete string
	// Trim and Lower normalize text as it is bound.
	Trim, Lower bool
	// Key marks the record's primary key, and Auto a value the database or
	// the program sets (a creation time): neither is ever a form field.
	Key, Auto bool
	// Messages are the field's own messages, by rule: "required", "max", …
	Messages map[string]string
}

// Choice is one value a field may take, and its label.
type Choice struct{ Value, Label string }

// Describer is a field attribute that describes its field.
type Describer interface{ Describe(*Spec) }

// Checker is a field attribute whose check needs the server: a value unique
// in a table. A form runs it as it is submitted, after the field's other
// rules passed; an error is the field's.
type Checker interface {
	Check(ctx context.Context, v Value) error
}

// Value is the field a Checker checks.
type Value struct {
	// Name is the field's Go name, Value its value.
	Name  string
	Value any
	// Record points to the record the form edits, as it was loaded (its
	// key included); nil when it edits none.
	Record any
}

// Float is a pointer to n, for Spec's Min and Max.
func Float(n float64) *float64 { return &n }

// Describe is the Spec attrs describe, in order: a later one's members
// over an earlier one's.
func Describe(attrs []any) Spec {
	var s Spec
	for _, a := range attrs {
		if d, ok := a.(Describer); ok {
			d.Describe(&s)
		}
	}
	return s
}

// SetMessage sets the field's message for rule.
func (s *Spec) SetMessage(rule, text string) {
	if text == "" {
		return
	}
	if s.Messages == nil {
		s.Messages = map[string]string{}
	}
	s.Messages[rule] = text
}

// Invalid is the error a Checker refuses a value with: the rule it broke
// ("unique") and, optionally, the message the form shows at the field (a
// form's default wording for the rule otherwise). Any other error is a
// failure to check, not a verdict on the value.
type Invalid struct{ Rule, Message string }

func (e *Invalid) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "the value breaks the rule " + e.Rule
}
