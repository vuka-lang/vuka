package orm

type FieldAttr interface{ fieldAttr() }

type ModelAttr interface{ modelAttr() }

type Char struct{ Max int }

func (Char) fieldAttr() {}

type Meta struct{ Table string }

func (Meta) modelAttr() {}

type Schema struct{}

func (s *Schema) Model(attrs ...ModelAttr)          {}
func (s *Schema) Field(ptr any, attrs ...FieldAttr) {}

type Model[T any] struct{ id int64 }
