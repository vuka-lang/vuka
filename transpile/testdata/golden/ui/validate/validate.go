// Package validate stands in for github.com/vuka-lang/ui/v2/validate.
package validate

import (
	"strconv"

	"vukatest.local/ui"
)

type (
	Required struct{}
	Max      float64
)

func (Required) Apply(o *ui.FieldOptions) { o.Validate = append(o.Validate, "required") }
func (m Max) Apply(o *ui.FieldOptions) {
	o.Validate = append(o.Validate, "max="+strconv.FormatFloat(float64(m), 'g', -1, 64))
}
