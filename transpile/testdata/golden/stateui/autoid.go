package stateui

import (
	"context"
	"reflect"
)

// AutoID is a component's id prop: one NewAutoID names when a tag leaves
// it out.
type AutoID string

// Suffix is the id of a part of it.
func (id AutoID) Suffix(part string) AutoID {
	if id == "" {
		return ""
	}
	return id + "-" + AutoID(part)
}

// NewAutoID is the id of the tag calling it; here always "auto".
func NewAutoID(ctx context.Context) AutoID { return "auto" }

// OmitZero is v, or nil when v is zero.
func OmitZero[T any](v T) any {
	if reflect.ValueOf(&v).Elem().IsZero() {
		return nil
	}
	return v
}
