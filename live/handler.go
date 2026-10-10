package live

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka"
)

var (
	ctxType   = reflect.TypeFor[context.Context]()
	errType   = reflect.TypeFor[error]()
	eventType = reflect.TypeFor[vuka.Event]()
	formType  = reflect.TypeFor[url.Values]()
)

// callHandler calls an event handler with the payload its parameter asks for:
//
//	func()                    nothing
//	func(string)              the value: the key of a keyboard event, else the element's value
//	func(bool), func(int), …  the value, parsed (a checkbox's value is "true" or "false"; "on" is true)
//	func(vuka.Event)          the whole event
//	func(url.Values)          the form's fields, as sent (also map[string][]string)
//	func(T), func(*T)         T a struct: the form's fields bound to its fields
//
// A leading context.Context gets the session's; the handler may return an error.
func callHandler(ctx context.Context, fn any, ev vuka.Event) (err error) {
	v := reflect.ValueOf(fn)
	if v.Kind() != reflect.Func || v.IsNil() {
		return fmt.Errorf("live: handler %s is %T, not a function", ev.Target, fn)
	}
	t := v.Type()
	var args []reflect.Value
	i := 0
	if t.NumIn() > 0 && t.In(0) == ctxType {
		args, i = append(args, reflect.ValueOf(ctx)), 1
	}
	switch t.NumIn() - i {
	case 0:
	case 1:
		a, err := payload(t.In(i), ev)
		if err != nil {
			return err
		}
		args = append(args, a)
	default:
		return fmt.Errorf("live: handler %s takes %d parameters; it takes one payload at most", ev.Target, t.NumIn())
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("live: handler %s panicked: %v", ev.Target, r)
		}
	}()
	return result(v.Call(args))
}

func result(out []reflect.Value) error {
	if len(out) == 1 && out[0].Type() == errType && !out[0].IsNil() {
		return out[0].Interface().(error)
	}
	return nil
}

func payload(t reflect.Type, ev vuka.Event) (reflect.Value, error) {
	switch {
	case t == eventType:
		return reflect.ValueOf(ev), nil
	case formType.ConvertibleTo(t) && t.Kind() == reflect.Map:
		return reflect.ValueOf(ev.Form).Convert(t), nil
	case t.Kind() == reflect.Struct:
		v := reflect.New(t)
		return v.Elem(), bindForm(v.Elem(), ev.Form)
	case t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct:
		v := reflect.New(t.Elem())
		return v, bindForm(v.Elem(), ev.Form)
	}
	s := ev.Value
	if ev.Key != "" && strings.HasPrefix(ev.Type, "key") {
		s = ev.Key
	}
	v := reflect.New(t).Elem()
	if err := setScalar(v, s); err != nil {
		return v, fmt.Errorf("live: %s event: %w", ev.Type, err)
	}
	return v, nil
}

func setScalar(v reflect.Value, s string) error {
	switch v.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Bool:
		b := s == "on"
		if !b && s != "" {
			var err error
			if b, err = strconv.ParseBool(s); err != nil {
				return fmt.Errorf("%q isn't true or false", s)
			}
		}
		v.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, v.Type().Bits())
		if err != nil {
			return fmt.Errorf("%q isn't a whole number", s)
		}
		v.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(strings.TrimSpace(s), 10, v.Type().Bits())
		if err != nil {
			return fmt.Errorf("%q isn't a whole number", s)
		}
		v.SetUint(n)
	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(strings.TrimSpace(s), v.Type().Bits())
		if err != nil {
			return fmt.Errorf("%q isn't a number", s)
		}
		v.SetFloat(n)
	default:
		return fmt.Errorf("can't take a %s", v.Type())
	}
	return nil
}

// bindForm sets v's fields from form: each field is named by its form tag, else
// its json tag, else its name, matched ignoring case; embedded structs' fields
// are v's own. A slice takes every value, anything else the first.
func bindForm(v reflect.Value, form url.Values) error {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		fv := v.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			if err := bindForm(fv, form); err != nil {
				return err
			}
			continue
		}
		if !f.IsExported() {
			continue
		}
		name := fieldName(f)
		if name == "-" {
			continue
		}
		vals := lookup(form, name)
		if vals == nil {
			continue
		}
		if fv.Kind() == reflect.Slice && fv.Type().Elem().Kind() != reflect.Uint8 {
			s := reflect.MakeSlice(fv.Type(), len(vals), len(vals))
			for j, x := range vals {
				if err := setScalar(s.Index(j), x); err != nil {
					return fmt.Errorf("live: field %s: %w", name, err)
				}
			}
			fv.Set(s)
			continue
		}
		if err := setScalar(fv, vals[0]); err != nil {
			return fmt.Errorf("live: field %s: %w", name, err)
		}
	}
	return nil
}

func fieldName(f reflect.StructField) string {
	for _, tag := range []string{"form", "json"} {
		if n, _, _ := strings.Cut(f.Tag.Get(tag), ","); n != "" {
			return n
		}
	}
	return f.Name
}

func lookup(form url.Values, name string) []string {
	if vs, ok := form[name]; ok {
		return vs
	}
	for k, vs := range form {
		if strings.EqualFold(k, name) {
			return vs
		}
	}
	return nil
}

// callUpdate delivers msg to c's Update method taking its type: Update itself,
// or one of its overloads (Vuka names them Update__T), each taking an optional
// context.Context and one message, returning nothing or an error. A method
// taking msg's type exactly wins over one it is only assignable to; of those,
// one taking a non-empty interface wins over one taking any.
func callUpdate(ctx context.Context, c vuka.Stateful, msg any) (bool, error) {
	v := reflect.ValueOf(c)
	mt := reflect.TypeOf(msg)
	var exact, assignable, catchAll []reflect.Value
	for i := 0; i < v.NumMethod(); i++ {
		m := v.Type().Method(i)
		if m.Name != "Update" && !strings.HasPrefix(m.Name, "Update__") {
			continue
		}
		ft := m.Type
		n := ft.NumIn() - 1
		first := 1
		if n == 2 && ft.In(1) == ctxType {
			n, first = 1, 2
		}
		if n != 1 || ft.NumOut() > 1 || ft.NumOut() == 1 && ft.Out(0) != errType {
			continue
		}
		pt := ft.In(first)
		switch {
		case mt == nil && (pt.Kind() == reflect.Interface || pt.Kind() == reflect.Pointer):
			catchAll = append(catchAll, v.Method(i))
		case mt == nil:
		case pt == mt:
			exact = append(exact, v.Method(i))
		case pt.Kind() == reflect.Interface && pt.NumMethod() == 0:
			catchAll = append(catchAll, v.Method(i))
		case mt.AssignableTo(pt):
			assignable = append(assignable, v.Method(i))
		}
	}
	for _, set := range [][]reflect.Value{exact, assignable, catchAll} {
		switch len(set) {
		case 0:
			continue
		case 1:
			m := set[0]
			arg := reflect.New(m.Type().In(m.Type().NumIn() - 1)).Elem()
			if msg != nil {
				arg.Set(reflect.ValueOf(msg))
			}
			args := []reflect.Value{arg}
			if m.Type().NumIn() == 2 {
				args = []reflect.Value{reflect.ValueOf(ctx), arg}
			}
			return true, result(m.Call(args))
		default:
			return false, fmt.Errorf("live: %s has %d Update methods taking %v", v.Type().Elem().Name(), len(set), mt)
		}
	}
	return false, nil
}
