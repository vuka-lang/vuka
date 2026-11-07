package field_test

import (
	"testing"

	"github.com/vuka-lang/vuka/v2/field"
)

type maxLen int

func (m maxLen) Describe(s *field.Spec) { s.Max = field.Float(float64(m)) }

type label string

func (l label) Describe(s *field.Spec) { s.Label = string(l) }

type required string

func (r required) Describe(s *field.Spec) {
	s.Required = true
	s.SetMessage("required", string(r))
}

// TestDescribe: the attributes describing a field apply in order, a later
// one's members over an earlier one's; others are ignored.
func TestDescribe(t *testing.T) {
	s := field.Describe([]any{maxLen(100), label("Name"), "not an attribute", maxLen(80), required("Enter a name"), required("")})
	if s.Max == nil || *s.Max != 80 || s.Label != "Name" || !s.Required || s.Messages["required"] != "Enter a name" {
		t.Errorf("Describe = %+v", s)
	}
	if s := field.Describe(nil); s.Max != nil || s.Required || s.Messages != nil {
		t.Errorf("Describe(nil) = %+v", s)
	}
}
